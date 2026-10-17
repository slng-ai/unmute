"""Faro's tools: find the learner, sign them up, and run the lesson.

Ported from unmute-core's examples/faro (curriculum.py, store.py and desk.py, in one
module so the package needs no import beyond state.py).

Every tool returns a typed result with ``said``: the exact line code says next, in the
learner's explanation language. The tool's YAML saves ``said`` to state and a reply says
it, so the model words only its own feedback and hints.

The model's part in a lesson is one typed call, ``grade_answer``: the learner's answer,
a grade from four, and one line of feedback. Code turns the grade into a result, records
the attempt in SQLite, moves the learner on or not, and says the next exercise. A model
cannot complete a lesson by saying so: only graded answers move progress.

The learners are in ``FARO_DB``, or ``faro.sqlite3`` in the working folder, made with
three learners the first time. The e2e run sets ``FARO_DB=:memory:``.

Authored material first (the lessons and every line code says), then the store, then the
tools.
"""

import json
import sqlite3
from dataclasses import dataclass
from pathlib import Path
from threading import Lock
from typing import Annotated, Literal

from pydantic import BaseModel, Field
from state import Lang
from unmute_core.settings import env_value
from unmute_core.state import Phone
from unmute_core.tools import ToolError

Goal = Literal["travel", "work", "daily"]
Level = Literal["A1", "A2", "B1", "B2"]


LANGUAGE_NAMES: dict[Lang, dict[Lang, str]] = {
    "ES": {"EN": "inglés", "ES": "español"},
    "EN": {"EN": "English", "ES": "Spanish"},
}
"""A target language's name, said in the explanation language."""


def target_of(record_id: str) -> Lang:
    """Return the language a lesson or item id teaches: ``en-travel-2-1`` teaches English."""
    return "EN" if record_id.startswith("en-") else "ES"


@dataclass(frozen=True)
class Item:
    """One practice item: what to do, in each explanation language. `check` marks the lesson's independent check."""

    id: str
    instruction: dict[Lang, str]
    check: bool = False


@dataclass(frozen=True)
class Lesson:
    """One lesson: its topic said in each explanation language, its can-do objective, and its items."""

    id: str
    topic: dict[Lang, str]
    objective: str
    items: tuple[Item, ...]


@dataclass(frozen=True)
class _Plan:
    """One lesson as written: topic in each language, objective, and (English, Spanish) instructions."""

    es: str
    en: str
    objective: str
    items: tuple[tuple[str, str], ...]


_GOALS: dict[Goal, tuple[_Plan, ...]] = {
    "travel": (
        _Plan(
            "presentarte",
            "introducing yourself",
            "I can introduce myself and say where I am from.",
            (
                ("Say hello and tell me your name.", "Saluda y dime cómo te llamas."),
                (
                    "Tell me where you are from and where you live now.",
                    "Dime de dónde eres y dónde vives ahora.",
                ),
                (
                    "Introduce yourself to a new friend at a hostel: your name, your city and one hobby.",
                    "Preséntate a alguien en un albergue: tu nombre, tu ciudad y una afición.",
                ),
            ),
        ),
        _Plan(
            "pedir en una cafetería",
            "ordering at a café",
            "I can order food and drink politely and ask for the total.",
            (
                (
                    "You are at a café. Ask politely for a drink.",
                    "Estás en una cafetería. Pide una bebida con educación.",
                ),
                (
                    "Now say how many you want: ask for two of something.",
                    "Ahora di cuántos quieres: pide dos de algo.",
                ),
                (
                    "Order two drinks and ask how much it is in total.",
                    "Pide dos bebidas y pregunta cuánto es en total.",
                ),
            ),
        ),
        _Plan(
            "resolver un problema en el hotel",
            "solving a problem at the hotel",
            "I can explain a simple problem and ask for help.",
            (
                (
                    "Tell the receptionist that your room is too cold.",
                    "Dile al recepcionista que tu habitación está muy fría.",
                ),
                (
                    "Ask if you can change to another room.",
                    "Pregunta si puedes cambiar de habitación.",
                ),
                (
                    "Your shower does not work. Explain the problem and ask when it will be fixed.",
                    "La ducha no funciona. Explica el problema y pregunta cuándo la arreglan.",
                ),
            ),
        ),
        _Plan(
            "practicar una conversación completa",
            "practising a full conversation",
            "I can handle a short travel conversation from start to finish.",
            (
                (
                    "Ask a stranger for directions to the train station.",
                    "Pregunta a alguien cómo llegar a la estación de tren.",
                ),
                (
                    "Buy a return ticket to the airport.",
                    "Compra un billete de ida y vuelta al aeropuerto.",
                ),
                (
                    "Check in at a hotel: give your name, ask about breakfast and say thank you.",
                    "Haz el registro en un hotel: di tu nombre, pregunta por el desayuno y da las gracias.",
                ),
            ),
        ),
    ),
    "work": (
        _Plan(
            "presentarte en el trabajo",
            "introducing yourself at work",
            "I can introduce myself and my role at work.",
            (
                ("Tell me your name and your job.", "Dime tu nombre y tu trabajo."),
                (
                    "Explain what you do on a normal work day.",
                    "Explica qué haces en un día normal de trabajo.",
                ),
                (
                    "Introduce yourself to a new colleague: your name, your role and your team.",
                    "Preséntate a un compañero nuevo: tu nombre, tu puesto y tu equipo.",
                ),
            ),
        ),
        _Plan(
            "hablar de lo que hiciste",
            "talking about what you did",
            "I can talk about completed work in the past tense.",
            (
                (
                    "Tell me three things you did yesterday at work.",
                    "Dime tres cosas que hiciste ayer en el trabajo.",
                ),
                (
                    "Tell me about a problem you solved last week.",
                    "Cuéntame un problema que resolviste la semana pasada.",
                ),
                (
                    "Give a short update: what you finished this week and what went wrong.",
                    "Da una actualización corta: qué terminaste esta semana y qué salió mal.",
                ),
            ),
        ),
        _Plan(
            "describir un proyecto",
            "describing a project",
            "I can describe a project: its goal, my part and its status.",
            (
                (
                    "Say what the goal of a project you work on is.",
                    "Di cuál es el objetivo de un proyecto en el que trabajas.",
                ),
                ("Say what your part in that project is.", "Di cuál es tu parte en ese proyecto."),
                (
                    "Describe the project to a new manager: its goal, your role and the next step.",
                    "Describe el proyecto a una jefa nueva: su objetivo, tu papel y el siguiente paso.",
                ),
            ),
        ),
        _Plan(
            "participar en una reunión",
            "taking part in a meeting",
            "I can give an opinion and agree or disagree politely in a meeting.",
            (
                (
                    "Say that you agree with an idea, and why.",
                    "Di que estás de acuerdo con una idea y por qué.",
                ),
                (
                    "Disagree politely with a suggestion.",
                    "Muestra tu desacuerdo con una propuesta, con educación.",
                ),
                (
                    "In a meeting, suggest a new deadline and give one reason.",
                    "En una reunión, propón una nueva fecha de entrega y da una razón.",
                ),
            ),
        ),
    ),
    "daily": (
        _Plan(
            "presentarte",
            "introducing yourself",
            "I can introduce myself and ask simple questions.",
            (
                ("Say hello and tell me your name.", "Saluda y dime cómo te llamas."),
                ("Ask me my name and where I live.", "Pregúntame cómo me llamo y dónde vivo."),
                (
                    "Introduce yourself to a neighbour and ask one question.",
                    "Preséntate a un vecino y hazle una pregunta.",
                ),
            ),
        ),
        _Plan(
            "ir de compras",
            "shopping",
            "I can ask for things in a shop and ask the price.",
            (
                (
                    "Ask for a kilo of apples at the market.",
                    "Pide un kilo de manzanas en el mercado.",
                ),
                ("Ask how much it costs.", "Pregunta cuánto cuesta."),
                (
                    "Buy bread and milk and ask how much it is in total.",
                    "Compra pan y leche y pregunta cuánto es en total.",
                ),
            ),
        ),
        _Plan(
            "hablar de tu rutina",
            "talking about your routine",
            "I can describe my daily routine.",
            (
                ("Say what time you get up.", "Di a qué hora te levantas."),
                ("Say what you do in the evening.", "Di qué haces por la tarde."),
                (
                    "Describe a normal Saturday, from morning to night.",
                    "Describe un sábado normal, de la mañana a la noche.",
                ),
            ),
        ),
        _Plan(
            "hacer planes",
            "making plans",
            "I can make and accept plans with a friend.",
            (
                ("Invite a friend to the cinema.", "Invita a un amigo al cine."),
                ("Suggest a time and a place to meet.", "Propón una hora y un lugar para quedar."),
                (
                    "Make a plan for Sunday with a friend: what, when and where.",
                    "Haz un plan para el domingo con un amigo: qué, cuándo y dónde.",
                ),
            ),
        ),
    ),
}
"""Four lessons per goal, written once. The last item of each is the independent check."""


def _built(target: Lang, goal: Goal, number: int, plan: _Plan) -> Lesson:
    lesson_id = f"{target.lower()}-{goal}-{number}"
    items = tuple(
        Item(f"{lesson_id}-{n}", {"EN": en, "ES": es}, check=n == len(plan.items))
        for n, (en, es) in enumerate(plan.items, start=1)
    )
    return Lesson(lesson_id, {"ES": plan.es, "EN": plan.en}, plan.objective, items)


LESSONS: dict[tuple[Lang, Goal], tuple[Lesson, ...]] = {
    (target, goal): tuple(_built(target, goal, n, plan) for n, plan in enumerate(plans, start=1))
    for target in ("EN", "ES")
    for goal, plans in _GOALS.items()
}
"""Every plan there is: four lessons per target language and goal, in this order."""


def lessons(target: Lang, goal: Goal) -> tuple[Lesson, ...]:
    """Return the four lessons of one plan, in order."""
    return LESSONS[target, goal]


def lesson(lesson_id: str) -> Lesson:
    """The lesson with this id, from any plan."""
    for plan in LESSONS.values():
        for found in plan:
            if found.id == lesson_id:
                return found
    raise KeyError(lesson_id)


def item(item_id: str) -> tuple[Lesson, Item]:
    """The item with this id and the lesson it belongs to."""
    lesson_id, _, _ = item_id.rpartition("-")
    found = lesson(lesson_id)
    return found, next(i for i in found.items if i.id == item_id)


GOAL_NAMES: dict[Lang, dict[Goal, str]] = {
    "ES": {"travel": "para viajar", "work": "para el trabajo", "daily": "para el día a día"},
    "EN": {"travel": "for travel", "work": "for work", "daily": "for daily life"},
}

NUMBERS: dict[Lang, dict[int, str]] = {
    "ES": {1: "primer", 2: "segundo", 3: "tercer"},
    "EN": {1: "first", 2: "second", 3: "third"},
}


def foreign_number(explanation: Lang) -> str:
    """Ask again for a number from outside Spain."""
    if explanation == "EN":
        return "Faro only takes Spanish numbers, starting with plus thirty-four. What is your Spanish number?"
    return "Faro solo acepta números españoles, que empiezan por más treinta y cuatro. ¿Cuál es tu número español?"


def signup_offer(explanation: Lang) -> str:
    """Offer a profile to a number that has none."""
    if explanation == "EN":
        return "I can't find a profile with that number. Would you like to create one and prepare a learning plan?"
    return "No encuentro un perfil con ese número. ¿Quieres crear uno y preparar un plan de aprendizaje?"


def status(
    name: str, explanation: Lang, lesson_id: str, item_index: int, previous: str, asked: bool
) -> str:
    """The returning learner's opening: where they stopped, and the two choices every return offers.

    `item_index` is the exercise waiting, and `asked` whether it was already
    put to them: either means the lesson is under way. `previous` is the
    lesson finished before this one, or ''.
    """
    topic = lesson(lesson_id).topic[explanation]
    ordinal = NUMBERS[explanation].get(item_index + 1, str(item_index + 1))
    before = lesson(previous).topic[explanation] if previous else ""
    if explanation == "EN":
        if item_index or asked:
            where = f"You were practising {topic}. We stopped at the {ordinal} exercise."
        elif before:
            where = f"You finished {before}. The next lesson, {topic}, is ready to start."
        else:
            where = f"Your first lesson, {topic}, is ready to start."
        return f"Hi, {name}. {where} Shall we carry on, or would you like a recap first?"
    if item_index or asked:
        where = f"Estabas practicando cómo {topic}. Nos quedamos en el {ordinal} ejercicio."
    elif before:
        where = f"Terminaste la lección de {before}. La siguiente, {topic}, está lista."
    else:
        where = f"Tu primera lección, {topic}, está lista."
    return f"Hola, {name}. {where} ¿Seguimos o prefieres repasar primero?"


def plan_proposal(target: Lang, goal: Goal, level: Level, minutes: int, explanation: Lang) -> str:
    """The exact words of a plan put to the learner for agreement: goal, level, the four lessons, the length."""
    topics = [plan_lesson.topic[explanation] for plan_lesson in lessons(target, goal)]
    language = LANGUAGE_NAMES[explanation][target]
    purpose = GOAL_NAMES[explanation][goal]
    if explanation == "EN":
        outline = f"{', '.join(topics[:-1])} and {topics[-1]}"
        return (
            f"We will start with {language} {level} {purpose}: {outline}. "
            f"Sessions will be about {minutes} minutes. Does that sound good?"
        )
    outline = f"{', '.join(topics[:-1])} y {topics[-1]}"
    return (
        f"Empezaremos con {language} {level} {purpose}: {outline}. "
        f"Serán sesiones de unos {minutes} minutos. ¿Te parece bien?"
    )


def plan_active(explanation: Lang) -> str:
    """Say that a plan is now active, and offer the first lesson."""
    if explanation == "EN":
        return "Done: your plan is active. Shall we start the first lesson now?"
    return "Hecho: tu plan ya está activo. ¿Empezamos ahora la primera lección?"


def exercise(explanation: Lang, item_id: str, *, again: bool = False) -> str:
    """An exercise as the learner hears it: in their own language, saying which language to answer in.

    The first time with where they are in the lesson; asked again, only the
    short retry, so a second try never repeats the whole introduction.
    """
    found, waiting = item(item_id)
    language = LANGUAGE_NAMES[explanation][target_of(item_id)]
    instruction = waiting.instruction[explanation]
    if again:
        return (
            f"Try again, in {language}: {instruction}"
            if explanation == "EN"
            else f"Otra vez, en {language}: {instruction}"
        )
    ordinal = NUMBERS[explanation].get(found.items.index(waiting) + 1, "")
    topic = found.topic[explanation].capitalize()
    if explanation == "EN":
        return f"{topic}, {ordinal} exercise. Answer in {language}: {instruction}"
    return f"{topic}, {ordinal} ejercicio. Responde en {language}: {instruction}"


def lesson_done(explanation: Lang, finished: str, following: str) -> str:
    """Say that a lesson is complete, and what comes next."""
    if explanation == "EN":
        if not following:
            return f"Lesson complete: {finished}. That was the last lesson of your plan. Well done!"
        return f"Lesson complete: {finished}. Next is {following}. Shall we go on, or stop here for today?"
    if not following:
        return f"Lección completada: {finished}. Era la última lección de tu plan. ¡Muy bien!"
    return f"Lección completada: {finished}. La siguiente es {following}. ¿Seguimos o lo dejamos por hoy?"


def next_step(explanation: Lang, item_id: str) -> str:
    """Where the next session starts, named by lesson and exercise number rather than by the prompt itself."""
    found, waiting = item(item_id)
    ordinal = NUMBERS[explanation].get(found.items.index(waiting) + 1, "")
    topic = found.topic[explanation]
    if explanation == "EN":
        return f"Next time: the {ordinal} exercise of {topic}."
    return f"La próxima vez: el {ordinal} ejercicio de {topic}."


def summary(explanation: Lang, learned: int, next_item: str) -> str:
    """What a session did, written by code from the store's count, for the plan's record. No model writes it."""
    if explanation == "ES":
        said = (
            f"Hoy has practicado {learned} ejercicio{'s' if learned != 1 else ''}."
            if learned
            else "Hoy hemos hablado sin hacer ejercicios nuevos."
        )
    else:
        said = (
            f"Today you practised {learned} exercise{'s' if learned != 1 else ''}."
            if learned
            else "Today we talked without new exercises."
        )
    return f"{said} {next_step(explanation, next_item)}" if next_item else said


def finished_plan(name: str, explanation: Lang) -> str:
    """A returning learner whose plan has no lesson left: the two things they can do."""
    if explanation == "ES":
        return f"Hola, {name}. Has completado todas las lecciones de tu plan. ¿Hacemos una evaluación nueva o repasamos?"
    return f"Hi, {name}. You have finished every lesson in your plan. Shall we reassess, or recap?"


def hello(name: str, explanation: Lang) -> str:
    """Say hello by name."""
    return f"Hola, {name}." if explanation == "ES" else f"Hi, {name}."


def noted(explanation: Lang) -> str:
    """Say that something was noted."""
    return "Anotado." if explanation == "ES" else "Noted."


def recap(explanation: Lang, done: list[str], review: list[str], following: str) -> str:
    """A recap of the plan as the store has it: what is done, what needs review, what comes next. Completes nothing."""
    if explanation == "ES":
        parts = [
            f"Has completado: {', '.join(done)}."
            if done
            else "Todavía no has completado ninguna lección.",
            f"Para repasar: {'; '.join(review)}."
            if review
            else "No tienes nada pendiente de repaso.",
            f"Lo siguiente: {following}." if following else "Has terminado tu plan.",
        ]
        return " ".join([*parts, "¿Seguimos?"])
    parts = [
        f"You have completed: {', '.join(done)}."
        if done
        else "You have not completed a lesson yet.",
        f"To review: {'; '.join(review)}." if review else "Nothing is waiting for review.",
        f"Next: {following}." if following else "You have finished your plan.",
    ]
    return " ".join([*parts, "Shall we carry on?"])


Outcome = Literal["independent", "assisted", "not_yet", "insufficient_evidence"]


class Attempt(BaseModel):
    """One answer to one exercise, and what it showed."""

    item: str
    answer: str
    result: Outcome


class Fact(BaseModel):
    """Something the learner told Faro about themselves, kept across calls."""

    key: str = Field(max_length=40)
    text: str = Field(max_length=200)


class Learner(BaseModel):
    """One learner: who they are, their plan, and how far they got.

    Attributes:
        lesson: The index of the lesson under way in the plan's lessons.
        item: The index of the exercise waiting in that lesson.
        completed: The ids of the lessons finished.
        review: The ids of the exercises practised with help or not yet right.
    """

    id: int
    phone: str
    name: str = Field(max_length=40)
    explanation: Lang
    target: Lang
    goal: Goal
    level: Level
    minutes: int = Field(default=10, ge=5, le=60)
    lesson: int = 0
    item: int = 0
    completed: list[str] = Field(default_factory=list)
    review: list[str] = Field(default_factory=list)
    attempts: list[Attempt] = Field(default_factory=list)
    memory: list[Fact] = Field(default_factory=list)

    @property
    def lessons(self) -> tuple[Lesson, ...]:
        """The plan's lessons, in order."""
        return lessons(self.target, self.goal)

    @property
    def finished(self) -> bool:
        """Whether every lesson of the plan is done."""
        return self.lesson >= len(self.lessons)

    @property
    def waiting(self) -> str:
        """The id of the exercise waiting, or empty once the plan is finished."""
        return "" if self.finished else self.lessons[self.lesson].items[self.item].id


def _seed(
    phone: str, name: str, explanation: Lang, target: Lang, goal: Goal, **rest: object
) -> dict:
    """Return one seeded learner's document."""
    return {
        "phone": phone,
        "name": name,
        "explanation": explanation,
        "target": target,
        "goal": goal,
        **rest,
    }


SEEDS = (
    _seed(
        "+34600111222",
        "Lucía",
        "ES",
        "EN",
        "travel",
        level="A2",
        lesson=1,
        item=1,
        completed=["en-travel-1"],
        memory=[{"key": "hobby", "text": "Enjoys hiking."}],
    ),
    _seed(
        "+34600333444",
        "Diego",
        "ES",
        "EN",
        "work",
        level="B1",
        lesson=2,
        completed=["en-work-1", "en-work-2"],
        review=["en-work-2-1"],
        memory=[{"key": "hobby", "text": "Enjoys cooking."}],
    ),
    _seed(
        "+34600555666",
        "Emma",
        "EN",
        "ES",
        "daily",
        level="A1",
        lesson=1,
        completed=["es-daily-1"],
        memory=[{"key": "prompts", "text": "Prefers short prompts."}],
    ),
)
"""Invented people. Any resemblance to a real one is a coincidence."""


class Store:
    """The learners, in one SQLite file. Thread-safe: tools may run in threads."""

    def __init__(self, path: str | Path = "faro.sqlite3") -> None:
        """Open the file, made with the seeded learners the first time.

        Args:
            path: The SQLite file, or ``:memory:`` for a store that lives as long as this object.
        """
        self._lock = Lock()
        self._db = sqlite3.connect(str(path), check_same_thread=False, isolation_level=None)
        with self._lock:
            self._db.execute(
                "CREATE TABLE IF NOT EXISTS learners ("
                "id INTEGER PRIMARY KEY, phone TEXT UNIQUE NOT NULL, body TEXT NOT NULL)"
            )
            for seed in SEEDS:
                self._db.execute(
                    "INSERT OR IGNORE INTO learners (phone, body) VALUES (?, ?)",
                    (seed["phone"], json.dumps(seed)),
                )

    def _learner(self, row: tuple[int, str] | None) -> Learner | None:
        """Return the learner a row holds, checked."""
        if row is None:
            return None
        return Learner.model_validate({**json.loads(row[1]), "id": row[0]})

    def find(self, phone: str) -> Learner | None:
        """Return the learner with this phone number, or None."""
        with self._lock:
            row = self._db.execute(
                "SELECT id, body FROM learners WHERE phone = ?", (phone,)
            ).fetchone()
        return self._learner(row)

    def get(self, learner_id: int) -> Learner:
        """Return one learner by id.

        Raises:
            KeyError: When no learner has that id.
        """
        with self._lock:
            row = self._db.execute(
                "SELECT id, body FROM learners WHERE id = ?", (learner_id,)
            ).fetchone()
        if (found := self._learner(row)) is None:
            raise KeyError(learner_id)
        return found

    def create(
        self, phone: str, name: str, explanation: Lang, target: Lang, goal: Goal, level: Level
    ) -> Learner:
        """Create a learner with a fresh plan.

        Raises:
            ValueError: When a learner already has that phone number.
        """
        body = _seed(phone, name, explanation, target, goal, level=level)
        with self._lock:
            try:
                cursor = self._db.execute(
                    "INSERT INTO learners (phone, body) VALUES (?, ?)", (phone, json.dumps(body))
                )
            except sqlite3.IntegrityError as error:
                raise ValueError("a profile with that phone number already exists") from error
        return Learner.model_validate({**body, "id": cursor.lastrowid})

    def save(self, learner: Learner) -> None:
        """Write a learner back, whole."""
        body = learner.model_dump(mode="json", exclude={"id"})
        with self._lock:
            self._db.execute(
                "UPDATE learners SET body = ? WHERE id = ?", (json.dumps(body), learner.id)
            )

    def count(self) -> int:
        """Return how many learners there are."""
        with self._lock:
            return self._db.execute("SELECT COUNT(*) FROM learners").fetchone()[0]


Grade = Literal["correct", "partly", "wrong", "unclear"]
RESULTS: dict[str, Outcome] = {
    "correct": "independent",
    "partly": "not_yet",
    "wrong": "not_yet",
    "unclear": "insufficient_evidence",
}
"""What each grade records. A correct answer after a hint records ``assisted`` instead."""

NumberSaid = Annotated[
    Phone,
    Field(
        description="The whole number the caller said, with its country code, like +34 600 111 222."
    ),
]


class Identified(BaseModel):
    """What finding or creating a learner returns: who they are, and code's next line."""

    status: Literal["known", "new", "created", "foreign"]
    said: str
    phone: str
    explanation: Lang
    learner_id: int | None = None
    learner_name: str | None = None
    notes: str | None = None


class Language(BaseModel):
    """What the prefetch of the caller's number returns, before anything is said."""

    explanation: Lang
    phone_spaced: str
    number_question: str


class Exercise(BaseModel):
    """What ``continue_lesson`` and ``grade_answer`` return: code's line, and what waits."""

    said: str
    waiting: str | None = None
    helped: bool = False
    lesson_complete: bool = False


class Hinted(BaseModel):
    """What ``give_hint`` returns: the hint, said by code, and that help was given."""

    helped: bool
    said: str


class Said(BaseModel):
    """A line code says, and the learner's notes as they stand now."""

    said: str
    notes: str | None = None


class Ended(BaseModel):
    """What ``end_call`` returns."""

    ended: bool


_STORE: Store | None = None


def store() -> Store:
    """Return the process's store, opened on first use from ``FARO_DB``."""
    global _STORE
    if _STORE is None:
        _STORE = Store(env_value("FARO_DB", str) or "faro.sqlite3")
    return _STORE


def not_spanish_number(phone: str, explanation: Lang) -> Identified | None:
    """Refuse a number from outside Spain before it is read back: Faro's learners are in Spain.

    The precheck of ``check_number``. Its reply says the line, in the explanation language.
    """
    if phone.startswith("+34"):
        return None
    return Identified(
        status="foreign",
        said=foreign_number(explanation),
        phone=phone,
        explanation=explanation,
    )


def brief(learner: Learner) -> str:
    """Write what code knows about a learner, one line each. Context, never instructions."""
    lines = [f"Learner: {learner.name}. Explains in {learner.explanation}."]
    lines += [f"Remembered ({fact.key}): {fact.text}" for fact in learner.memory]
    done = [lesson(lesson_id).topic["EN"] for lesson_id in learner.completed]
    lines.append(
        f"Plan: learning {learner.target} {learner.goal}, level {learner.level}, "
        f"{learner.minutes}-minute sessions."
    )
    lines.append(f"Lessons completed: {', '.join(done) or 'none yet'}.")
    return "\n".join(lines)


def created_line(learner: Learner) -> str:
    """Say a new learner's plan, in their language, and offer the first lesson."""
    language = LANGUAGE_NAMES[learner.explanation][learner.target]
    purpose = GOAL_NAMES[learner.explanation][learner.goal]
    topics = [plan_lesson.topic[learner.explanation] for plan_lesson in learner.lessons]
    if learner.explanation == "EN":
        outline = f"{', '.join(topics[:-1])} and {topics[-1]}"
        return (
            f"Nice to meet you, {learner.name}. Your plan is {language} {learner.level} "
            f"{purpose}: {outline}. Shall we start the first lesson now?"
        )
    outline = f"{', '.join(topics[:-1])} y {topics[-1]}"
    return (
        f"Encantado, {learner.name}. Tu plan es {language} {learner.level} {purpose}: "
        f"{outline}. ¿Empezamos ahora la primera lección?"
    )


def _learner(learner_id: int, explanation: Lang) -> Learner:
    """Return a learner, with the language this call explains in."""
    learner = store().get(learner_id)
    if learner.explanation != explanation:
        learner = learner.model_copy(update={"explanation": explanation})
        store().save(learner)
    return learner


def language_of(phone: str) -> Language:
    """Return the language to explain in for a caller's number, before the greeting.

    Nothing about the learner is said or shown: the number is not confirmed yet. The
    result also carries the greeting's number read-back, in that language.
    """
    found = store().find(phone)
    digits = phone.removeprefix("+34")
    spaced = f"+34 {digits[:3]} {digits[3:6]} {digits[6:]}" if phone.startswith("+34") else phone
    explanation: Lang = found.explanation if found else "EN"
    question = (
        "Let me check: is your number"
        if explanation == "EN"
        else "A ver si lo tengo bien: ¿tu número es"
    )
    return Language(
        explanation=explanation, phone_spaced=spaced, number_question=f"{question} {spaced}?"
    )


def _identify(phone: str, explanation: Lang) -> Identified:
    """Return who the caller is, and the line that opens their call."""
    learner = store().find(phone)
    if learner is None:
        return Identified(
            status="new", said=signup_offer(explanation), phone=phone, explanation=explanation
        )
    learner = _learner(learner.id, explanation)
    if learner.finished:
        said = finished_plan(learner.name, explanation)
    else:
        previous = learner.completed[-1] if learner.completed else ""
        said = status(
            learner.name,
            explanation,
            learner.lessons[learner.lesson].id,
            learner.item,
            previous,
            False,
        )
    return Identified(
        status="known",
        said=said,
        phone=phone,
        explanation=explanation,
        learner_id=learner.id,
        learner_name=learner.name,
        notes=brief(learner),
    )


def use_this_number(phone: str, explanation: Lang) -> Identified:
    """The caller said yes to the number read back in the greeting: look them up."""
    return _identify(phone, explanation)


def check_number(phone: NumberSaid, explanation: Lang) -> Identified:
    """Look the caller up by the number they said. It first reads the number back.

    Call it again with the same number after their yes.
    """
    return _identify(phone, explanation)


def create_profile(
    name: Annotated[
        str, Field(min_length=1, max_length=40, description="What they want to be called.")
    ],
    learn: Annotated[
        Literal["English", "Spanish"], Field(description="The language they want to learn.")
    ],
    goal: Annotated[
        Literal["travel", "work", "daily-life"],
        Field(description="What it is for: travel, work or daily life."),
    ],
    level: Annotated[
        Level, Field(description="Their level: A1 for a beginner, A2 for some, B1 for confident.")
    ],
    phone: str,
    explanation: Lang,
) -> Identified:
    """Create the new learner's profile and plan. It first reads it back.

    Call it again with the same values after their yes.
    """
    target: Lang = "EN" if learn == "English" else "ES"
    purpose: Goal = "daily" if goal == "daily-life" else goal
    if target == explanation:
        raise ToolError(
            "They would learn the language they already explain in. Ask which language "
            "they want to learn: English or Spanish."
        )
    try:
        learner = store().create(phone, name.strip(), explanation, target, purpose, level)
    except ValueError as error:
        raise ToolError(str(error)) from error
    return Identified(
        status="created",
        said=created_line(learner),
        phone=phone,
        explanation=explanation,
        learner_id=learner.id,
        learner_name=learner.name,
        notes=brief(learner),
    )


def continue_lesson(learner_id: int, explanation: Lang) -> Exercise:
    """Put the next exercise of their plan to the learner. Code says it."""
    learner = _learner(learner_id, explanation)
    if learner.finished:
        return Exercise(said=finished_plan(learner.name, explanation))
    return Exercise(said=exercise(explanation, learner.waiting), waiting=learner.waiting)


def give_hint(
    hint: Annotated[
        str,
        Field(
            min_length=1,
            max_length=300,
            description=(
                "What you say to help, in the explanation language: an answer to their "
                "question, a slower explanation or a hint. Never the whole answer."
            ),
        ),
    ],
    waiting: str,
) -> Hinted:
    """Help with the exercise waiting. The application says your hint, so say nothing else.

    A right answer after a hint counts as practised with help.
    """
    return Hinted(helped=bool(waiting), said=hint.strip())


def grade_answer(
    answer: Annotated[
        str,
        Field(
            min_length=1,
            max_length=600,
            description="The learner's attempt at the exercise, copied from what they said.",
        ),
    ],
    grade: Annotated[
        Grade,
        Field(
            description=(
                "correct: does what was asked, in the target language, and can be "
                "understood; small slips are fine. partly: in the target language, but "
                "does only part of it or needs a real fix. wrong: does not do what was "
                "asked, or is mostly not in the target language. unclear: too cut off "
                "or garbled to judge. Judge meaning, never spelling or pronunciation."
            )
        ),
    ],
    feedback: Annotated[
        str,
        Field(
            min_length=1,
            max_length=200,
            description=(
                "Said before the next line, in the explanation language. correct: brief "
                "praise. partly or wrong: what was wrong, then the right sentence in the "
                "target language. unclear: ask them to say it again."
            ),
        ),
    ],
    learner_id: int,
    explanation: Lang,
    waiting: str,
    helped: bool,
) -> Exercise:
    """Grade the learner's attempt at the exercise waiting. Code says what follows."""
    learner = _learner(learner_id, explanation)
    if learner.waiting != waiting:
        # The exercise the model graded is not the one the store has waiting.
        raise ToolError("that exercise is no longer waiting: call continue_lesson")
    result = RESULTS[grade]
    if result == "independent" and helped:
        result = "assisted"
    attempts = [*learner.attempts, Attempt(item=waiting, answer=answer, result=result)]
    review = list(learner.review)
    if result in ("assisted", "not_yet") and waiting not in review:
        review.append(waiting)
    current = learner.lessons[learner.lesson]
    moved = result in ("independent", "assisted")
    update: dict[str, object] = {"attempts": attempts, "review": review}
    if moved and learner.item == len(current.items) - 1:
        update |= {
            "lesson": learner.lesson + 1,
            "item": 0,
            "completed": [*learner.completed, current.id],
        }
    elif moved:
        update |= {"item": learner.item + 1}
    learner = learner.model_copy(update=update)
    store().save(learner)
    if moved and learner.item == 0:
        following = "" if learner.finished else learner.lessons[learner.lesson].topic[explanation]
        done = lesson_done(explanation, current.topic[explanation], following)
        return Exercise(said=f"{feedback} {done}", lesson_complete=True)
    line = exercise(explanation, learner.waiting, again=not moved)
    return Exercise(said=f"{feedback} {line}", waiting=learner.waiting)


def read_progress(learner_id: int, explanation: Lang) -> Said:
    """Say what the learner has done, what needs review and what comes next. Changes nothing."""
    learner = _learner(learner_id, explanation)
    done = [lesson(lesson_id).topic[explanation] for lesson_id in learner.completed]
    review = [item(item_id)[1].instruction[explanation].rstrip(".") for item_id in learner.review]
    following = "" if learner.finished else learner.lessons[learner.lesson].topic[explanation]
    return Said(said=recap(explanation, done, review, following))


def remember(
    key: Annotated[
        str, Field(min_length=1, max_length=40, description="A short stable name, like hobby.")
    ],
    text: Annotated[
        str,
        Field(
            min_length=1,
            max_length=200,
            description='What they told you, in one short sentence, like "Enjoys climbing."',
        ),
    ],
    learner_id: int,
    explanation: Lang,
) -> Said:
    """Remember something the learner told you about themselves, for later calls.

    A correction uses the same key as the fact it replaces.
    """
    learner = _learner(learner_id, explanation)
    key = key.strip().lower()
    memory = [
        *(fact for fact in learner.memory if fact.key != key),
        Fact(key=key, text=text.strip()),
    ]
    learner = learner.model_copy(update={"memory": memory[-20:]})
    store().save(learner)
    return Said(said=noted(explanation), notes=brief(learner))


def end_call() -> Ended:
    """End the call once the learner says they are done. Say your goodbye in the same reply."""
    return Ended(ended=True)
