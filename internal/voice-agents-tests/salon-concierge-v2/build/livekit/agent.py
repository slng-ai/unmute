import asyncio
import inspect
import json
import logging
import os
import re
import time
from dataclasses import dataclass, field
from datetime import datetime
from typing import Annotated, Literal
from urllib.parse import quote
from zoneinfo import ZoneInfo
import tools.cancel_booking
import tools.check_availability
import tools.create_booking
import tools.find_or_create_customer
import tools.list_bookings
import tools.look_up_customer
import tools.modify_booking
import tools.record_complaint
from dotenv import load_dotenv

from pydantic import AfterValidator, BaseModel, Field, TypeAdapter, ValidationError
from livekit import api
from livekit import rtc
from livekit.agents import (
    NOT_GIVEN,
    Agent,
    AgentTask,
    AgentServer,
    AgentSession,
    JobContext,
    JobProcess,
    NotGivenOr,
    RunContext,
    TurnHandlingOptions,
    function_tool,
    get_job_context,
    inference,
    llm,
    metrics,
)
from livekit.agents.voice import MetricsCollectedEvent
from livekit.plugins import openai, silero, slng

from dev_metrics import install_dev_metrics
import knowledge
from tracing import setup_langfuse


logger = logging.getLogger("salon-concierge-v2")
logger.setLevel(logging.INFO)

load_dotenv()


# --- prompts ---------------------------------------------------------------

COMPLAINT_SPECIALIST_PROMPT = """# Sage and Stone customer care

You are still Robin, the same person the caller has been talking to. Nothing
about the call changed for them, so nothing about you changes either. You listen
to the complaint, acknowledge the impact, record the useful facts, and give a
clear next step. A human manager is available to inbound phone callers through
the manager transfer.

## How you speak

A text to speech voice reads out everything you write, exactly as you write it.
So write speech, not text.

- Whole sentences in ordinary capitalization. No markdown, no asterisks, no
  bullet points, no headings, no emoji, no symbols: the voice reads them out
  loud.
- Never send a bare fragment. A number or an amount sits inside a sentence.
- Capitals are read letter by letter, so use them only when that is what you
  want, like ATM.
- Write money, dates, times and numbers the plain written way and let the voice
  say them: 3:00 PM, Friday the 12th, 28 euros, 20 percent. Where the refund
  policy writes an amount or a deadline out in words, quote it as written.
- Write a phone number as a plus sign and the usual digit groups. Never put
  commas between digits and never break a number into separate words: the voice
  drops everything after the first comma.
- One or two short sentences a turn, one question at a time.
- Never say agent names, tool names, result keys, or raw results.

## How you sound

Calm, unhurried, and on the caller's side. Someone is telling you something
went wrong, so the warmth matters more here than anywhere else in the call.

- Use contractions, and vary your opener. "Right, ...", "Okay, ...", "Mhm,
  ...", "Ah, ...", "I see, ...", or no opener at all.
- A short filler at the front of a turn sounds like a person thinking, and it
  rides at the front of a turn that also does its job.
- A genuine apology is the one place to let the tone drop. "Oh, that's not
  okay, I'm sorry." Do not perform it and do not repeat it.
- Never gush, never say "I completely understand", and never thank the caller
  for their patience.

## What you never do

- You join a conversation that is already running. Continue it: never open with
  a greeting, an introduction, or a question already answered.
- Run a handoff or an escalation silently, and never mention one.
- Keep complaint IDs silent. Never promise a refund, credit, callback time, or
  policy that is not in the conversation.
- Never ask the caller for their phone number and never say one back. This
  prompt holds no number on purpose: if a step needs it, it already has it.
- Never claim a complaint was recorded or a transfer started unless the matching
  action ran in the same turn and succeeded.

## Escalation comes first

Call the manager transfer immediately when the caller asks for a manager,
supervisor, owner or human, or is clearly and strongly frustrated: repeated
anger after an attempted resolution, hostile language, or refusing to continue
with an agent. Do not verify anyone first, because reaching a person is never
gated on identifying yourself.

Ordinary disappointment, a firm tone, or one negative adjective is not strong
frustration. This is a conversation judgment, not a sentiment score.

If there is no active phone leg, say a direct transfer needs an inbound phone
call. If a real call reaches the carrier but the manager cannot be connected,
call it a carrier failure rather than a browser limitation, and never promise
the caller will stay connected.

## Complaint workflow

Listen first. Identify last, and only because a record needs an owner.

1. Acknowledge the problem without admitting facts the caller did not state.
2. Ask only for the missing service or visit detail and what they would like
   done. Quote the refund policy from the documents freely here: none of it
   depends on knowing who is calling.
3. A complaint needs a number to attach to. Read the conversation info at the
   end of this prompt: once it names a customer, verification has already
   succeeded, so say nothing about it and go straight to recording. Otherwise
   run verification, saying why in one short sentence.
4. Then run the complaint step in the same turn, silently. It records what the
   caller told you and hands back what happened.
5. When it hands its result back, give the smallest useful next step in one
   short sentence, without repeating what it already said. Offer a manager when
   the request needs a person with authority.

   Say the resolution it recorded, in the tense it recorded it. A refund or a
   redo that was offered is offered, so "a free redo can be arranged" and "I
   can have a manager confirm that" are both true, and "your appointment
   tomorrow will be used as the redo" is not: nothing in this call approved it,
   and a caller who hangs up believing it arrives expecting a free visit.
6. If the caller changes to booking help or another topic, hand back to the
   concierge immediately and silently.

Conversation info:
What this call has established so far. Read it rather than re-reading the conversation, and never ask for something already recorded here.
1. Booking date: {{booking_date}}
2. Caller reason: {{caller_reason}}
3. Customer: {{customer}}
4. Appointments: {{appointments}}
5. Complaints: {{complaints}}"""

CONCIERGE_PROMPT = """# Sage and Stone concierge

You are Robin, on the front desk at Sage and Stone. You are the person the
caller talks to for the whole call. You confirm who is calling, run the booking
step yourself, answer what you can, and hand over only for the one thing you do
not own: complaints and refunds, which customer care handles because it holds
the refund policy and the complaint record and you must not.

## How you speak

A text to speech voice reads out everything you write, exactly as you write it.
So write speech, not text.

- Whole sentences in ordinary capitalization. No markdown, no asterisks, no
  bullet points, no headings, no emoji, no symbols: the voice reads them out
  loud.
- Never send a bare fragment. A number or an amount sits inside a sentence.
- Capitals are read letter by letter, so use them only when that is what you
  want, like ATM.
- Write money, dates, times and numbers the plain written way and let the voice
  say them: 3:00 PM, Friday the 12th, 28 euros, 20 percent. Where the salon's
  own documents write an amount out in words, quote them as written.
- Write a phone number the way it is written on a phone: a plus sign, then the
  country code, then the rest in groups of two to four digits. Never put commas
  between digits and never break a number into separate words: the voice drops
  everything after the first comma.
- One or two short sentences a turn, one question at a time.
- Never say agent names, tool names, result keys, or raw results.

## How you sound

Relaxed, warm, and quick. You have worked this desk for years, you are talking
to one person, and you are not reading a script.

- Use contractions, and start a sentence with And, But or So when it fits.
- Vary your opener, and never open two turns in a row the same way. "Right,
  ...", "Okay, so ...", "Mhm, ...", "Ah, ...", "Lovely, ...", or no opener at
  all.
- A small filler at the front of a turn sounds like a person thinking. After a
  standalone "um", follow it with "so". A filler rides at the front of a turn
  that also does its job: never send a turn that is only a filler.
- One short line while a lookup runs is fine and human. "Let me check." Never
  ask the caller to hold, and never say the same line twice in a row.
- If a better phrasing lands mid sentence, drop the first one and carry on with
  the second, without apologising for it.
- When you did not catch something, say so plainly. "Sorry, I missed that, say
  it again?"

## What you never do

- Run a handoff or an escalation silently. Never mention a handoff, a
  specialist, or a routing step: just move.
- Keep internal IDs silent, and never say the caller's phone number. The
  verification step is the only place a number is spoken, and it is the only
  prompt that holds one.
- Never claim something happened unless the matching action ran in this turn
  and succeeded.
- Never invent salon policy, availability, or customer details.
- Never say the same thing twice in a call unless the caller asks you to.

## Workflow

1. If they ask for a manager or a person, or they are clearly and strongly
   frustrated, escalate on this turn. Do not verify first and do not ask for a
   number: somebody who wants a person should not be interviewed to get one.
   Say what actually happened. If there is no active phone leg, say a direct
   transfer needs an inbound phone call, and never tell the caller to phone the
   salon: on a real call they already have. If a phone call reaches the carrier
   but the manager cannot be connected, call it a carrier failure rather than a
   browser limitation, and never promise the caller will stay connected.
2. Otherwise work out whether they need booking help, have a complaint, or want
   to chat. Ask only if it is unclear, and never ask something they already
   said.
3. A complaint goes to customer care straight away.
4. Booking needs verification first: it reads the number back and needs a yes,
   and the booking step will not start without it. Once verification succeeds,
   run the booking step in the same turn, silently. If it does not succeed, say
   what the practical problem is once and offer to try again.
5. When the booking step hands its result back, confirm it in one short
   sentence that names nothing. "You're all set." "That's booked." "Done, it's
   in the diary." The step already said the service, the day and the time, so
   naming them again is the same news twice, and naming them from the
   conversation info is worse: those are the bookings this call already made,
   not the one that just happened.
6. Send the booking step back in for every booking change the caller wants
   making, including a second one in the same call: each visit records one
   booking, so two bookings is two visits. A step handing back an unserved
   request has given you a job, not an excuse, so act on it in the same turn
   and never apologise for it. What does not go back to the step is a question
   about a booking already on the conversation info: that is yours to answer
   from the info, in one sentence, with no step and no tool.

Verification happens once per call, and keeping it to once is your job rather
than the step's: the step runs with no conversation in front of it, so it
cannot tell that it already ran. Read the conversation info at the end of this
prompt. Once it names a customer, verification has already succeeded, so carry
on with what it found and never run the step again unless the caller says the
number is wrong.

## Answering things yourself

Anything that is not a booking and not a complaint, you handle here. Prices,
services and opening hours come from the salon's own documents, so look them up
rather than remembering them. For anything outside those documents, say plainly
that you cannot check it. Never claim to have searched or browsed a live
source.

Read the conversation info below rather than re-reading the call. It is the
record of what this call has already established, and it is why you never need
to ask again for something already on it.

Conversation info:
What this call has established so far. Read it rather than re-reading the conversation, and never ask for something already recorded here.
1. Booking date: {{booking_date}}
2. Caller reason: {{caller_reason}}
3. Customer: {{customer}}
4. Appointments: {{appointments}}
5. Complaints: {{complaints}}"""

HANDLE_COMPLAINT_PROMPT = """# Record one complaint

You write one complaint down, with what has already been offered about it.

The specialist has the refund policy and has already quoted it. You do not: the
only tool you have here records the complaint.

## How you speak

A text to speech voice reads out everything you write, exactly as you write it.
So write speech, not text.

- Whole sentences in ordinary capitalization. No markdown, no asterisks, no
  bullet points, no emoji, no symbols: the voice reads them out loud.
- Never send a bare fragment. A number or an amount sits inside a sentence.
- Capitals are read letter by letter, so use them only when that is what you
  want.
- Write money, dates and times the plain written way and let the voice say
  them: 28 euros, 20 percent, Friday the 12th. Where the policy already writes
  an amount or a deadline out in words, quote it exactly as written.
- One or two short sentences a turn, one question at a time. Never say tool
  names or result keys, and keep the complaint id silent.

## How you sound

Same person the caller has been talking to, still calm and on their side.
Nothing about the call changed for them when this step started, so nothing
about you changes either. Use contractions and vary your opener. A genuine
apology is the one place to let the tone drop: do not perform it and do not
repeat it. Never gush, never say "I completely understand", and never thank the
caller for their patience.

## What you are handed

Both sides of the conversation so far, tool records left out, so whatever the
caller told the specialist about what went wrong is already in front of you.
Never ask them to repeat it. The conversation info at the end of this prompt
holds the caller's record and any appointment this call already booked, moved,
or cancelled.

## What you never do

- The caller is already verified. Never ask for their name or number.
- Never promise a refund, credit, callback time, or policy that is not in the
  conversation.
- Never say a complaint is recorded unless the matching tool ran in this turn
  and said so.

## Workflow

1. The caller has already described what went wrong, so your first response
   records it. Never open by asking what happened, and only ask a question if
   something you genuinely need is missing.
2. Read back through the conversation for what is settled: what they are
   unhappy about, which visit it concerns, and what the specialist already
   offered. Never invent an offer and never quote a policy: you do not have it
   here. Where nothing has been offered yet, the resolution is that it has been
   noted, which is a real answer.
3. Record the complaint with that resolution, then say in one short sentence
   that it is written down. If it ever comes back saying the record failed, say
   plainly that it was not recorded rather than implying it was.
4. Give the smallest useful next step in one short sentence. Offer a manager
   when the request needs a person with authority.

## What you return

**The complaint.** Built from what you just recorded:

- `complaint_id`: from what the tool returned.
- `reason`: sorted into the salon's own categories: service quality, waiting
  time, price, staff, or other.
- `about`: the appointment the complaint concerns, matched to one the
  conversation info already shows, action and all. Leave it out for anything
  else, including an older visit this call never recorded.
- `resolution`: what has been offered or done on this call. `noted` is for when
  nothing more specific was offered, not for when recording failed. The other
  three are offers, so record the offer you actually made: `rebooking_offered`
  when you said a redo can be arranged, never when you told the caller a
  booking they already hold is now that redo. Nothing here approves anything.

**The reason they rang.** `complain`, always.

**The summary.** One short line for whoever reads this next: recorded with what
was offered, or not recorded and why. Plain words, not something you would say
out loud.

Conversation info:
What this call has established so far. Read it rather than re-reading the conversation, and never ask for something already recorded here.
1. Booking date: {{booking_date}}
2. Caller reason: {{caller_reason}}
3. Customer: {{customer}}
4. Appointments: {{appointments}}
5. Complaints: {{complaints}}

When this step is complete, call `finish` with: complaint, reason, summary.

`unserved_request` is for a request this step cannot serve. Do this step's own work first, and never use it to skip that work: the caller's original reason for being here is not an unserved request. If a handoff here covers what they want, call that handoff instead. Only when no tool and no handoff here can serve what the caller is asking, call `finish` with the closest result you have and their request in `unserved_request`, in their own words, rather than refusing or explaining what you cannot do here. The agent that owns this step reads that field and takes the caller from there."""

MANAGE_BOOKING_PROMPT = """# Handle one booking change

You take one booking request from start to finish: work out what the caller
wants, get one clear yes, then save it.

## How you speak

A text to speech voice reads out everything you write, exactly as you write it.
So write speech, not text.

- Whole sentences in ordinary capitalization. No markdown, no asterisks, no
  bullet points, no emoji, no symbols: the voice reads them out loud.
- Never send a bare fragment. A time sits inside a sentence: "Friday at 11:30 AM
  works.", never "11:30." on its own.
- Capitals are read letter by letter, so use them only when that is what you
  want.
- Write dates and times the plain written way and let the voice say them:
  11:30 AM, 3:00 PM, tomorrow, Friday the 12th.
- Name a day once, and the way the caller named it. If they said tomorrow, say
  tomorrow.
- Say `haircolor` as "hair color", `haircut_and_haircolor` as "a haircut and a
  hair color", and `dry_cut` as "a dry cut".
- Offer times the way a person does: "I've got 9:00 AM, 11:30, or 3:00 in the
  afternoon." Never read out a list.
- One or two short sentences a turn, one question at a time. Never say tool
  names or result keys, and keep booking IDs silent.

## How you sound

Same person the caller has been talking to. Quick, warm, and a bit pleased when
a booking lands. Use contractions, vary your opener, and never say the same
information twice.

## What you are handed

Today is `{{booking_date}}`, in the salon's own timezone. You get what was said
out loud on this call plus the conversation info at the end of this prompt. No
tool result anybody ran before you is in front of you, so call the tool
yourself for availability, a booking list or a price.

## What you never do

- The caller is already verified. Never ask for their name or number.
- Use only the bookings and slots a tool returned. Never invent an ID and never
  improvise a time nobody offered.
- Never say a booking is saved, moved, or cancelled unless the matching tool ran
  in this turn and said so.

## Workflow

1. The caller is waiting on you, so your first response always speaks. Read what
   they have already told you and ask only for what is genuinely missing. A
   caller who said "a haircut tomorrow afternoon" has given you the service, the
   day and the part of the day, so ask nothing and go straight to availability.
2. Work out create, modify, or cancel from what they said. Ask only if it is
   unclear. This is the `action` you record at the end.
3. To modify or cancel, list their bookings first, unless the record was created
   during this call: a new record has nothing on it, so say there is nothing
   booked yet and offer to make one. Do the same if an existing record's list
   comes back empty. If more than one booking fits, name them by service and
   time and let the caller pick.
4. To create or modify, work the day out from the date above rather than asking
   a tool what day it is, then check availability and offer up to three of the
   times it returned, narrowed to the part of the day they asked for. A caller
   who said afternoon does not want to hear about 9:00 AM. Never ask which time
   suits them and then read out the times: if the next thing you do is check
   availability, check it.
5. Say the whole thing back in one sentence and ask one yes-or-no question: the
   service, the day, the time. "Tomorrow at 3:00 PM for a haircut, shall I book
   it?" When only one time fits, that is the same sentence as the offer, not a
   second one. Nothing said before that question counts as a yes.
6. On a clear yes, save it in the same turn with `confirmed` set to true, then
   say it landed in one short sentence. "That's booked." is the whole turn: the
   caller heard the day, the time and the service in your own question and said
   yes to them.
7. On a no, ask what they would like instead and offer again. Do not record an
   appointment for something that did not happen.
8. Finish once the booking you were asked for is saved, or once there is truly
   nothing left this step can do. There is no "still working" finish: while the
   conversation is live, speak instead. And never finish having done nothing:
   you were sent in because the caller wants a booking change, so that change
   is your job, however far into the call it arrives.

## What you return

**The appointment.** The one booking you saved in this visit, built from the
tool that just ran: `scheduled_date` and `scheduled_time`, the
`appointment_type` in the salon's own words, the `action`, and the `booking_id`
the tool returned.

Leave it out entirely when you saved nothing this visit. A caller who asked and
then changed their mind leaves nothing to record, and an appointment already on
the conversation info was recorded by an earlier visit: handing it back again
is the same booking counted twice, not a new one. Only ever return a booking a
tool saved for you in this visit.

**The reason they rang.** `create_booking`, `modify_booking`, or
`cancel_booking`, from what they asked you. Never ask for it and never say it
out loud. Return it even when nothing was saved.

**The summary.** One short line for whoever reads this next: booked, moved,
cancelled, or not confirmed. Plain words, not something you would say out loud.

## Leaving this step

**They raise a complaint or ask for a person.** Call `to_complaints` on the same
turn and save nothing. This is the only handoff you hold. If they ask for a
manager, customer care reaches one; you cannot.

**They ask for something a booking tool cannot do, once a booking is saved.**
A price, an opening time, anything that is not a booking change. Put it in
`unserved_request` when you finish, in their own words, and Robin takes it from
there.

A second booking is not that. It is a booking change, so it is yours: do it.
What you never do is save two of them in one visit, because you hand back one
appointment and the other would go unrecorded. So save one, say it landed,
finish, and Robin sends you straight back in for the next one.

Never reach for a handoff or `unserved_request` because you are unsure what to
say. Ask them instead.

Conversation info:
What this call has established so far. Read it rather than re-reading the conversation, and never ask for something already recorded here.
1. Booking date: {{booking_date}}
2. Caller reason: {{caller_reason}}
3. Customer: {{customer}}
4. Appointments: {{appointments}}
5. Complaints: {{complaints}}

When this step is complete, call `finish` with: appointment, reason, summary.

`unserved_request` is for a request this step cannot serve. Do this step's own work first, and never use it to skip that work: the caller's original reason for being here is not an unserved request. If a handoff here covers what they want, call that handoff instead. Only when no tool and no handoff here can serve what the caller is asking, call `finish` with the closest result you have and their request in `unserved_request`, in their own words, rather than refusing or explaining what you cannot do here. The agent that owns this step reads that field and takes the caller from there."""

VERIFY_CUSTOMER_PROMPT = """# Verify the customer

You confirm who you are speaking to, and you have two ways in.

**When you already have a number**, which is most inbound calls: the number is
`{{customer_phone}}` and the name on that record is `{{customer_name}}`. Read
the number back, ask for a yes, and stop. Never ask for a number you already
have.

**When you have nothing**, because the caller withheld their number or the
route does not carry one, both come through blank and you ask for a number.

**You are handed no conversation.** This step runs with the history reset, so
you have this prompt, the values above and the conversation info at the end.
Nothing the caller said is in front of you, and neither is anything you did on
an earlier run. So you cannot tell whether verification already happened: Robin
decides that and Robin can see it.

You also cannot tell why the caller rang, and you do not need to. **Never ask
what they are calling about.** They have already said it, and the step that
acts on their request reads the conversation and records the reason itself.

## How you speak

A text to speech voice reads out everything you write, exactly as you write it.
So write speech, not text.

- Whole sentences in ordinary capitalization. No markdown, no asterisks, no
  emoji, no symbols: the voice reads them out loud.
- Never send a bare fragment. A number sits inside a short question, never as
  digits on their own.
- Capitals are read letter by letter, so use them only when that is what you
  want.
- Write a phone number the way it is written on a phone: a plus sign, then the
  country code, then the rest in groups of two to four digits. The voice
  recognises that shape.
- Never break a number into separate words and never put commas between digits.
  A comma inside a run of digits stops the voice: "plus 3 4, 1 1 1, 1 1 1" came
  out as "plus three four" and the caller heard nothing to check.
- One short sentence, one question. Never say tool names or result keys.

## How you sound

Same person the caller has been talking to, still relaxed. Reading a number
back is the dullest moment of the call, so keep it light and keep it moving.
Use contractions, vary how you open, and never say the same sentence twice in
this step. No apologies for the process and no thanking them for their
patience.

## Workflow

1. The call is already running and the caller is waiting on you, so your first
   response always speaks: either read back the number above or ask for one.
   Never open with silence and never open by asking what they wanted.
2. **If `{{customer_phone}}` holds a number, read it back.** Do not say where it
   came from and never say the name: a caller ringing from a friend's phone
   would hear a stranger's name, which is the worst thing this step can do. If
   it is empty, ask for the number, keeping any digits they already gave.
3. Read every digit back once, written as a phone number, and ask if that is
   right. Group the digits yourself in the usual groups of two to four, and
   never copy the pauses out of what you heard: a caller who trails off
   mid-number is transcribed as "111 11 1", and reading that back makes a whole
   number look one digit short. Never invent a country code.
4. Agreement is a yes however it arrives: "yes", "that's right", "sounds about
   right", or agreement followed by the caller moving straight on to what they
   came for. Only a correction or a plain no is not a yes.
5. On a no, take the new digits and read back again in different words. A caller
   who hears their own question repeated word for word thinks the line broke. A
   no to a number you were handed is not a mistake: somebody on a friend's phone
   says no here and is right to, so drop it and ask for the one they want.
6. You never decide whether a number is long enough. The lookup does, and a
   number it cannot use comes back with an invalid status. So on a yes, call the
   lookup with the digits you are holding, whatever shape they are in. Most of
   the world's numbers are not three, three and four, and one that looks wrong
   to you is almost always whole.
7. If the lookup still returns invalid after one retry, or the caller will not
   confirm, finish with an empty phone number and a customer record whose status
   is invalid.
8. On a yes and a usable number you are done. Finish.

## What you return

**The confirmed number.** In E.164 and no other shape: a plus sign, then the
digits, with nothing between them, no spaces, no brackets and no dashes. Copy
what the lookup returned character for character: do not regroup it, do not pretty it
up, do not drop the plus. This value is data, not something to say out loud; the
readback in step 3 is the only place a number is spoken, and it is spoken in the
spaced shape.

**The customer record.** The confirmed number and the status the lookup gave
you, existing, created, or invalid. The number is the identity here, so the
record carries no name and no id: never add either. On an invalid number still
return a record, with the invalid status.

**The summary.** One short line for whoever reads this next: confirmed and
looked up, confirmed but invalid, or not confirmed. Plain words, not something
you would say out loud.

Conversation info:
What this call has established so far. Read it rather than re-reading the conversation, and never ask for something already recorded here.
1. Booking date: {{booking_date}}
2. Caller reason: {{caller_reason}}
3. Customer: {{customer}}
4. Appointments: {{appointments}}
5. Complaints: {{complaints}}
6. Customer phone: {{customer_phone}}

When this step is complete, call `finish` with: customer, customer_phone, summary.

`unserved_request` is for a request this step cannot serve. Do this step's own work first, and never use it to skip that work: the caller's original reason for being here is not an unserved request. If a handoff here covers what they want, call that handoff instead. Only when no tool and no handoff here can serve what the caller is asking, call `finish` with the closest result you have and their request in `unserved_request`, in their own words, rather than refusing or explaining what you cannot do here. The agent that owns this step reads that field and takes the caller from there."""


# --- cold transfer destination -----------------------------------------------
# TransferSIPParticipant's transfer_to is a URI, not a bare number: it becomes
# the Refer-To of the outgoing SIP REFER. `tel:+E164` covers Twilio and Telnyx;
# a provider that needs its trunk host in the Refer-To (Plivo) takes the
# `sip:+E164@<trunk>` form, which a destination may already carry (SCHEMA N26).
def _refer_uri(destination: str) -> str:
    """The destination as a REFER URI, adding `tel:` only to a bare number."""
    if destination.startswith(("tel:", "sip:", "sips:")):
        return destination
    return "tel:" + destination


# --- required environment ----------------------------------------------------
# Everything this agent needs to run: the model providers' keys, the connection
# to the orchestrator, every address and token a tool or MCP source names, and
# anything else the package declared. Derived from what the compiler knows it
# requires rather than from the author's `secrets:` block, so a package that
# declares nothing still refuses to start without them. A missing one fails the
# session before the agent answers, rather than at the first tool call.
#
# The phone route's own credentials are deliberately absent: one file serves
# every channel, and demanding carrier credentials would refuse a browser
# session on a phone package. They are listed in .env.example and the runbook.
REQUIRED_ENV = [
    "LANGFUSE_BASE_URL",
    "LANGFUSE_PUBLIC_KEY",
    "LANGFUSE_SECRET_KEY",
    "OPENAI_API_KEY",
    "SLNG_API_KEY",
]


def require_env() -> None:
    missing = [name for name in REQUIRED_ENV if not os.getenv(name)]
    if missing:
        raise RuntimeError(
            "Missing required environment variables: " + ", ".join(missing)
        )


# A browser session has no phone leg to cold-transfer. Check these only after
# the entry path identifies a real carrier call, and before its greeting.
CALL_REQUIRED_ENV = [
    "MANAGER_PHONE_NUMBER",
]


def require_call_env() -> None:
    missing = [name for name in CALL_REQUIRED_ENV if not os.getenv(name)]
    if missing:
        raise RuntimeError(
            "Missing environment for a phone call: " + ", ".join(missing)
        )


# --- templates ---------------------------------------------------------------
_TEMPLATE = re.compile(r"\{\{\s*([a-z_][a-z0-9_]*)\s*\}\}")


def _render(text: str, userdata, *, quote_values: bool = False) -> str:
    """Substitute each variable token from the session userdata (SCHEMA 4.4).
    Only substituted values are URL-encoded, never the surrounding literal."""
    if userdata is None:
        return text

    def _one(match: re.Match[str]) -> str:
        value = getattr(userdata, match.group(1), None)
        # Through _state_text, so a declared value renders as compact JSON and
        # an empty one as words. Anything not declared structured comes back
        # exactly as str() gave it.
        value = _state_text(match.group(1), value)
        return quote(value, safe="") if quote_values else value

    return _TEMPLATE.sub(_one, text)


def _refusal(tool: str, userdata, needed: list[tuple[str, str]]) -> str:
    """Refuse a call whose injected variables are still unset (V4): the model is
    told what to ask for, and no half-formed request is ever sent."""
    unset = [
        (name, hint)
        for name, hint in needed
        if getattr(userdata, name, None) in (None, "")
        # A value the caller has not agreed to is present and not usable. Without
        # this, a pre-fetched number would reach somebody else's record: the
        # request would go out against a proposal rather than a fact.
        or name in getattr(userdata, "_unconfirmed", ())
    ]
    if not unset:
        return ""
    names = ", ".join(name for name, _ in unset)
    hints = " ".join(f"{name}: {hint}" for name, hint in unset if hint)
    return f"cannot call {tool} yet: {names} not set. Ask the caller first. {hints}".strip()


# Prerequisite guard, generated by internal/generate/guard.go.
#
# A control that declares requires: is held back until every named variable
# holds a value. The refusal below goes to the model, never to the caller: it
# names what is missing and which control supplies it, so the model fetches the
# value and retries within the same turn. The caller hears the model's next
# natural question and never learns a guard fired.
#
# Both emitted targets render this same block, so their wording cannot drift.
_PREREQUISITE_LIMIT = 5
_PREREQUISITE_SUPPLIER = {
    "appointments": "manage_booking",
    "caller_reason": "handle_complaint",
    "complaints": "handle_complaint",
    "customer": "verify_customer",
    "customer_phone": "verify_customer",
}

# Every name declared as a list. An empty one means nothing has been
# recorded yet, which is exactly the state a guard exists to wait for, so it
# is unmet. Tested by the declared type rather than by truthiness, because 0,
# False and 0.0 are real answers a caller can give and treating them as
# missing is the bug this predicate was written to avoid.
_PREREQUISITE_LISTS = {"appointments", "caller_reason", "complaints"}


def _unmet_prerequisites(state, names):
    unmet = []
    for name in names:
        root, _, path = name.partition(".")
        # A value awaiting the caller's agreement satisfies no guard through any
        # path into it: the mark is on the value, so naming a field one level
        # down cannot escape it. getattr with a default, because the set is
        # created by the pre-fetch and a path here may never have run one.
        if root in getattr(state, "_unconfirmed", ()):
            unmet.append(name)
            continue
        value = getattr(state, root, None)
        for step in path.split(".") if path else ():
            if value is None:
                break
            value = (
                value.get(step)
                if isinstance(value, dict)
                else getattr(value, step, None)
            )
        if value is None or value == "":
            unmet.append(name)
        elif not path and root in _PREREQUISITE_LISTS and len(value) == 0:
            unmet.append(name)
    return unmet


def _prerequisite_refusal(names, at_limit):
    wants = ", ".join(
        name + " (call " + _PREREQUISITE_SUPPLIER[name] + " to get it)"
        if name in _PREREQUISITE_SUPPLIER
        else name
        for name in names
    )
    if at_limit:
        return (
            "Not started. Still missing: "
            + wants
            + ". Do not say any of this out loud. You have tried several times"
            + " without it, so ask the caller for it directly now, in your own"
            + " plain words, and stay in the conversation."
        )
    return (
        "Not started. Missing: "
        + wants
        + ". Do not say any of this out loud. Get the missing value now, then"
        + " call this again in the same turn."
    )


# One counter per guarded control per session. Held on the module rather than the
# agent because a handoff replaces the agent object and a caller who keeps
# refusing across a handoff has not started over.
_prerequisite_refusals: dict[str, int] = {}


# Pre-fetch, generated by internal/generate/prefetch.go.
#
# Facts that are knowable before the greeting are resolved here, once per call,
# so the model is never asked to discover them. An entry whose inputs are empty
# is skipped and the values keep their declared defaults, which is what makes a
# package that pre-fetches a carrier fact still work on a route that supplies
# none. Nothing here can fail a call.
_PREFETCH_BUDGET_S = 2.0
_PREFETCH_VALUE_MAX = 512
_PREFETCH_TZ = ZoneInfo("Europe/Madrid")


def _prefetch_call_facts(call_context):
    """The call's own facts, with the local seed filling only what the carrier did not.

    UNMUTE_CALL_FACTS is what `unmute dev --source name=value` sets. The
    carrier wins wherever it supplied a value: a seed stands in for a fact it
    could not supply, and never overrides one it did, so a stale value in a .env
    cannot quietly reshape a real call.

    Merging here rather than at each call site is deliberate. This driver starts a
    session from four different places, and a merge written four times is a merge
    that disagrees with itself in one of them.
    """
    facts = dict(call_context or {})
    seeded = os.environ.get("UNMUTE_CALL_FACTS")
    if not seeded:
        return facts
    try:
        values = json.loads(seeded)
        for name in values:
            if not facts.get(name):
                facts[name] = values[name]
    except (ValueError, TypeError):
        logger.warning(
            "UNMUTE_CALL_FACTS is not a JSON object of call facts; ignoring it"
        )
    return facts


def _prefetch_bounded(name, value):
    """Bound one pre-fetched value, and say so when it is shortened.

    The length is only knowable here, at run time, so this cannot be a
    compile-time refusal. Silence is the one thing it must not be.
    """
    text = "" if value is None else str(value)
    if len(text) <= _PREFETCH_VALUE_MAX:
        return text
    logger.warning(
        f"prefetch: {name} was {len(text)} characters and is cut to "
        f"{_PREFETCH_VALUE_MAX}; a value this long stops the prompt being cached"
    )
    return text[:_PREFETCH_VALUE_MAX]


async def _prefetch(state, call_context) -> None:
    call_context = _prefetch_call_facts(call_context)
    # Every value awaiting the caller's agreement. The prerequisite guard reads
    # this set, so an unconfirmed value satisfies no step, and each generated
    # assign write discards its own name as the caller settles it.
    state._unconfirmed = set()
    # Entries run in the order agent.yaml lists them. Nothing here is sorted or
    # reordered: an entry reading a value a later entry assigns was refused at
    # compile time, so by here the order is known good.

    # today: clock -> booking_date
    state.booking_date = datetime.now(_PREFETCH_TZ).date().isoformat()
    logger.info(f"prefetch today: resolved booking_date={state.booking_date}")

    # caller: source from_number -> customer_phone
    _value = (call_context or {}).get("from_number") or ""
    if not _value:
        logger.info("prefetch caller: skipped, the call carries no from_number")
    else:
        state.customer_phone = _prefetch_bounded("customer_phone", _value)
        state._unconfirmed.add("customer_phone")
        logger.info("prefetch caller: resolved customer_phone, awaiting confirmation")

    # profile: look_up_customer(phone={{customer_phone}}) -> customer_name
    if not state.customer_phone:
        logger.info("prefetch profile: skipped, customer_phone is empty")
    else:
        try:
            async with asyncio.timeout(_PREFETCH_BUDGET_S):
                result = tools.look_up_customer.look_up_customer(
                    phone=state.customer_phone
                )
                if inspect.isawaitable(result):
                    result = await result
            state.customer_name = _prefetch_bounded(
                "customer_name", (result or {}).get("name")
            )
            state._unconfirmed.add("customer_name")
            logger.info(
                "prefetch profile: resolved customer_name, awaiting confirmation"
            )
        except TimeoutError:
            logger.warning(
                f"prefetch profile: gave up after {_PREFETCH_BUDGET_S}s; customer_name keeps its default"
            )
        except Exception:
            logger.exception(
                "prefetch profile: failed; customer_name keeps its default"
            )


# --- declared state ----------------------------------------------------------
# Generated from the `shapes:` and the typed `variables:` in agent.yaml. Both
# target frameworks already depend on Pydantic, so nothing here adds one.
#
# Emitted from one place in the compiler for both targets, so the classes, the
# checks and the refusal wording cannot differ between them.

_SHAPE_PHONE = re.compile(r"^\+[1-9]\d{6,14}$")


def _shape_phone(value: str) -> str:
    # Empty is not a wrong value, it is no value yet. It is what a declared
    # variable holds before anything fills it, what the state block renders as
    # words, and what a tool hands back for a field it could not fill. Refusing
    # it here deadlocked a live call on both targets: the model had nothing else
    # to send, so every retry was refused the same way and the step never
    # finished. A wrong value is still refused; an absent one is not wrong.
    if value and not _SHAPE_PHONE.match(value):
        raise ValueError(
            "expected a phone number in E.164, one leading plus and 7 to 15 digits, like +34600111222"
        )
    return value


# AfterValidator and never a pattern= constraint: a pattern reaches the schema
# the model is sent, one target's strict converter keeps it, and the provider
# rejects it. So the schema says str and the shape is checked here.
#
# The description is how the format reaches the model at all, and it is the only
# keyword that can: it travels as prose, so no strict converter strips it. A
# field that said nothing about its shape was learned from a refusal mid-call,
# which cost a model round trip on every value the prompt spells one way and
# this type another. A field carrying its own description keeps that one; the
# emitter appends this phrase to it.
Phone = Annotated[
    str,
    AfterValidator(_shape_phone),
    Field(
        description="a phone number in E.164, one leading plus and 7 to 15 digits, like +34600111222"
    ),
]

_SHAPE_DATE = re.compile(r"^\d{4}-\d{2}-\d{2}$")


def _shape_date(value: str) -> str:
    # Empty is not a wrong value, it is no value yet. It is what a declared
    # variable holds before anything fills it, what the state block renders as
    # words, and what a tool hands back for a field it could not fill. Refusing
    # it here deadlocked a live call on both targets: the model had nothing else
    # to send, so every retry was refused the same way and the step never
    # finished. A wrong value is still refused; an absent one is not wrong.
    if value and not _SHAPE_DATE.match(value):
        raise ValueError("expected a day written year-month-day, like 2026-03-19")
    return value


# AfterValidator and never a pattern= constraint: a pattern reaches the schema
# the model is sent, one target's strict converter keeps it, and the provider
# rejects it. So the schema says str and the shape is checked here.
#
# The description is how the format reaches the model at all, and it is the only
# keyword that can: it travels as prose, so no strict converter strips it. A
# field that said nothing about its shape was learned from a refusal mid-call,
# which cost a model round trip on every value the prompt spells one way and
# this type another. A field carrying its own description keeps that one; the
# emitter appends this phrase to it.
Date = Annotated[
    str,
    AfterValidator(_shape_date),
    Field(description="a day written year-month-day, like 2026-03-19"),
]

_SHAPE_TIME = re.compile(r"^([01]\d|2[0-3]):[0-5]\d$")


def _shape_time(value: str) -> str:
    # Empty is not a wrong value, it is no value yet. It is what a declared
    # variable holds before anything fills it, what the state block renders as
    # words, and what a tool hands back for a field it could not fill. Refusing
    # it here deadlocked a live call on both targets: the model had nothing else
    # to send, so every retry was refused the same way and the step never
    # finished. A wrong value is still refused; an absent one is not wrong.
    if value and not _SHAPE_TIME.match(value):
        raise ValueError(
            "expected a time of day on the 24-hour clock, like 09:30 or 17:45"
        )
    return value


# AfterValidator and never a pattern= constraint: a pattern reaches the schema
# the model is sent, one target's strict converter keeps it, and the provider
# rejects it. So the schema says str and the shape is checked here.
#
# The description is how the format reaches the model at all, and it is the only
# keyword that can: it travels as prose, so no strict converter strips it. A
# field that said nothing about its shape was learned from a refusal mid-call,
# which cost a model round trip on every value the prompt spells one way and
# this type another. A field carrying its own description keeps that one; the
# emitter appends this phrase to it.
Time = Annotated[
    str,
    AfterValidator(_shape_time),
    Field(description="a time of day on the 24-hour clock, like 09:30 or 17:45"),
]

_SHAPE_ID = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$")


def _shape_id(value: str) -> str:
    # Empty is not a wrong value, it is no value yet. It is what a declared
    # variable holds before anything fills it, what the state block renders as
    # words, and what a tool hands back for a field it could not fill. Refusing
    # it here deadlocked a live call on both targets: the model had nothing else
    # to send, so every retry was refused the same way and the step never
    # finished. A wrong value is still refused; an absent one is not wrong.
    if value and not _SHAPE_ID.match(value):
        raise ValueError(
            "expected an identifier: letters, digits, and then any of dot, dash, underscore or colon"
        )
    return value


# AfterValidator and never a pattern= constraint: a pattern reaches the schema
# the model is sent, one target's strict converter keeps it, and the provider
# rejects it. So the schema says str and the shape is checked here.
#
# The description is how the format reaches the model at all, and it is the only
# keyword that can: it travels as prose, so no strict converter strips it. A
# field that said nothing about its shape was learned from a refusal mid-call,
# which cost a model round trip on every value the prompt spells one way and
# this type another. A field carrying its own description keeps that one; the
# emitter appends this phrase to it.
Id = Annotated[
    str,
    AfterValidator(_shape_id),
    Field(
        description="an identifier: letters, digits, and then any of dot, dash, underscore or colon"
    ),
]


class Appointment(BaseModel):
    """One thing being booked, moved or cancelled."""

    scheduled_date: Date
    scheduled_time: Time
    appointment_type: Annotated[
        Literal["haircut", "haircolor", "haircut_and_haircolor", "dry_cut"],
        Field(
            description="The service the caller asked for, in the salon's own words."
        ),
    ]
    action: Annotated[
        Literal["create", "modify", "cancel"],
        Field(description="What this call did to this appointment."),
    ]
    booking_id: Annotated[
        Id | None,
        Field(
            description="The diary's own id for the booking, once one exists. Absent while the caller is still choosing a time. Expected an identifier: letters, digits, and then any of dot, dash, underscore or colon."
        ),
    ]


class Complaint(BaseModel):
    """One thing the caller is unhappy about."""

    complaint_id: Id
    reason: Annotated[
        Literal["service_quality", "waiting_time", "price", "staff", "other"],
        Field(
            description="What the complaint is about, in the salon's own categories."
        ),
    ]
    about: Annotated[
        Appointment | None,
        Field(
            description="The appointment the complaint concerns, when it concerns one. A complaint about the salon in general has none."
        ),
    ]
    resolution: Annotated[
        Literal["refund_offered", "rebooking_offered", "escalated", "noted"],
        Field(
            description="What was offered or done about it on this call. Three of these are offers and not outcomes: a refund offered is not a refund approved, a rebooking offered is not a rebooking confirmed, and escalated means somebody will look, not that they agreed. Only what a tool did is done. Say this value out loud in the tense it is written in."
        ),
    ]


class Customer(BaseModel):
    """Who the caller is, as the salon's records have them."""

    phone_number: Phone
    status: Annotated[
        Literal["existing", "created", "invalid"],
        Field(
            description="What the lookup found for the confirmed number: existing for a record that was already there, created for one written during this call, and invalid for a number the lookup could not use."
        ),
    ]


class _StateRefused(Exception):
    """A value that does not fit its declared type, refused where it enters.

    Carried as an exception rather than a return so the write cannot happen by
    accident: the previous contents stay exactly as they were, and the message
    goes back to the model, which is what lets it correct itself on the next
    turn instead of the step recording something wrong.
    """

    def __init__(self, message: str) -> None:
        super().__init__(message)
        self.message = message


def _typed(field, adapter, value):
    """Validate one value entering the declared state."""
    try:
        return adapter.validate_python(value)
    except ValidationError as error:
        first = error.errors()[0]
        where = ".".join(str(part) for part in first["loc"])
        named = f"{field}.{where}" if where else field
        raise _StateRefused(f"{named}: {first['msg']}") from None


def _append_entry(entries, value):
    """One entry onto a declared list, unless it is already on it.

    A step re-entered mid-call can read a value out of its own state block and
    hand it straight back, which is not a second thing happening. One live call
    entered the booking step four times and finished three of them immediately,
    each with the same appointment it had recorded on the first, so one booking
    became four entries and the caller's recap listed a booking four times.

    An object carries its own identity, so an identical one is the same thing
    reported twice. A plain value is not: two bookings really do give two
    reasons of "create_booking", and both of those count. So the skip is for
    structured entries only.

    Nothing absent is added either, which is how a step that concluded nothing
    this time finishes without inventing an entry.
    """
    if value is None:
        return
    if isinstance(value, (dict, list)) and value in entries:
        return
    entries.append(value)


def _plain(value):
    """A validated value as plain data.

    Plain data is the only shape both frameworks accept back from a tool: one
    refuses a BaseModel outright and drops the whole tool result with a log
    line, the other cannot serialise one at all.
    """
    if isinstance(value, BaseModel):
        return value.model_dump(mode="json")
    if isinstance(value, list):
        return [_plain(entry) for entry in value]
    if isinstance(value, dict):
        return {key: _plain(entry) for key, entry in value.items()}
    return value


def _schema(adapter):
    """One declared type's schema, with every $ref resolved into place.

    Pydantic emits $defs and a $ref for a shape that contains another shape, and
    this is not a formatting preference. Measured on one real request to the
    provider, three ways:

    - the schema as Pydantic emits it, nested inside one tool property with no
      strict flag: accepted with a 200, and the model invented field names for
      the nested object because it never read the definition. Every result would
      then have been refused where it entered, on every call.
    - the same schema with the refs inlined: accepted, and the model filled the
      shape's own fields exactly, the nullable one included.
    - the shape the other target sends, with the $defs hoisted to the
      parameters root and strict on: accepted, and correct. A $defs anywhere but
      that root is a 400 naming the pointer.

    This target nests the schema inside one property and sends no strict flag,
    so it is the first case unless the refs are resolved here.
    """
    schema = adapter.json_schema()
    defs = schema.pop("$defs", {})

    def resolve(node):
        if isinstance(node, list):
            return [resolve(item) for item in node]
        if not isinstance(node, dict):
            return node
        target = node.get("$ref")
        if isinstance(target, str) and target.startswith("#/$defs/"):
            found = defs.get(target.rsplit("/", 1)[1], {})
            siblings = {key: value for key, value in node.items() if key != "$ref"}
            return {**resolve(found), **siblings}
        return {key: resolve(value) for key, value in node.items()}

    return resolve(schema)


_FINISH_TYPES = {
    "handle_complaint": {
        "complaint": TypeAdapter(Complaint),
        "reason": TypeAdapter(
            Literal[
                "create_booking",
                "modify_booking",
                "cancel_booking",
                "request_informations",
                "complain",
            ]
        ),
    },
    "manage_booking": {
        "appointment": TypeAdapter(Appointment | None),
        "reason": TypeAdapter(
            Literal[
                "create_booking",
                "modify_booking",
                "cancel_booking",
                "request_informations",
                "complain",
            ]
        ),
    },
    "verify_customer": {
        "customer": TypeAdapter(Customer),
        "customer_phone": TypeAdapter(Phone),
    },
}


def _typed_result(step, values):
    """Validate a step's declared results where they enter the state.

    Refused here rather than carried into a later step that assumes it is
    right, and refused on both targets rather than on the one whose framework
    happens to validate tool arguments: one of them validates through Pydantic
    and lets the model self-correct, the other splats raw JSON into the handler.
    """
    adapters = _FINISH_TYPES.get(step)
    if not adapters:
        return values
    out = dict(values)
    for name, adapter in adapters.items():
        # Absent goes through the adapter too, rather than being skipped. A
        # field the model left out is a field with no value, and that is what a
        # prompt telling it to leave one out asks for: a value that may be
        # absent validates as None and the append drops it, and a value that
        # may not is refused here with the message that lets the model correct
        # itself. Skipping an absent field instead left the key missing from
        # the result, and the assignment that reads it by name raised a
        # KeyError inside the finish handler on the target whose framework
        # validates no argument of its own.
        out[name] = _plain(_typed(name, adapter, out.get(name)))
    return out


_STATE_STRUCTURED = {
    "appointments",
    "booking_date",
    "caller_reason",
    "complaints",
    "customer",
    "customer_phone",
}
_STATE_EMPTY = "none recorded yet."
# The bound on one rendered value, in characters. The same number the router
# bounds a template variable by, because this is the same value travelling the
# same way, and one number cannot be two.
_STATE_VALUE_MAX = 4000


def _state_text(name, value):
    """One value as a prompt reads it.

    Compact JSON for anything declared structured, never a Python repr: a repr
    writes single quotes and None, which is not JSON and is not what any
    provider produced. Words for a declared value with no contents, so a step
    cannot mistake "not yet known" for "known to be nothing".

    A value that was never declared structured renders exactly as it did before
    this existed, which is what keeps every package written before it unchanged.
    """
    if name in _STATE_STRUCTURED:
        if value is None or value == "" or value == [] or value == {}:
            return _STATE_EMPTY
        if not isinstance(value, str):
            value = json.dumps(_plain(value), separators=(",", ":"), ensure_ascii=False)
    text = "" if value is None else str(value)
    if len(text) > _STATE_VALUE_MAX:
        # The length is only knowable here, at run time, so this cannot be a
        # compile-time refusal. What it must not be is silent: a shortened value
        # is a value the model reads as complete. An f-string rather than a
        # placeholder, because this line is emitted into two modules that log
        # through two different libraries and either style prints literally on
        # the other one.
        logger.warning(
            f"declared state: {name} rendered {len(text)} characters and is shortened to "
            f"{_STATE_VALUE_MAX}; a value this long also stops the prompt being cached"
        )
        text = text[:_STATE_VALUE_MAX]
    return text


# --- shared state ------------------------------------------------------------
# Typed session state (SCHEMA 4.4): tasks assign into it, transfers read it.
@dataclass
class Userdata:
    appointments: list[Appointment] = field(
        default_factory=list
    )  # What this call booked, moved or cancelled, in the order the caller gave them. A list because a caller can do two things in one call, and the booking step appends rather than replacing, so the second does not erase the first. It starts empty, which is why a guard naming it waits: an empty list is nothing recorded yet, not a decision the caller made.
    booking_date: Date | None = (
        ""  # Today's date in the salon's own timezone, YYYY-MM-DD. Read once from the clock before the greeting, so a caller saying "tomorrow" costs one model request instead of two chained tool calls. A call that crosses midnight keeps the day it started on, which is deliberate: a date changing underneath a conversation would leave the caller and the agent disagreeing about what "tomorrow" means halfway through.
    )
    caller_reason: list[
        Literal[
            "create_booking",
            "modify_booking",
            "cancel_booking",
            "request_informations",
            "complain",
        ]
    ] = field(
        default_factory=list
    )  # Why the caller rang. A list, because one call can do more than one thing: somebody who books and also complains has two reasons, and each step appends the one it heard rather than replacing what the last step recorded.
    complaints: list[Complaint] = field(
        default_factory=list
    )  # What the caller was unhappy about, one entry per thing, each with what was offered about it. Appended by the complaint step for the same reason the appointments are.
    customer: Customer | None = (
        None  # Who the caller is, once the verification step has heard them agree to the number and looked the record up. Absent until then, and that absence is load-bearing: the booking step's guard names customer.status, so with no value the step waits rather than starting on a caller nobody looked up. No `default:` for the same reason a default was wrong on the flat status value it replaced. A default is a value the variable holds before the first word, so a defaulted customer satisfies the guard on an empty record.
    )
    customer_name: str | None = (
        ""  # The name on the record the caller's number belongs to, looked up before the greeting. Inherits `customer_phone`'s confirming step, because a name found from a number nobody has agreed to is exactly as unconfirmed as that number was: greeting a stranger by the account holder's name is the worst thing this feature could do, and the compiler refuses the prompt that would.
    )
    customer_phone: Phone | None = (
        ""  # The caller's phone number in E.164: a plus sign, then digits, with no spaces, brackets or dashes. One shape for every phone number in this package, the MANAGER_PHONE_NUMBER transfer destination included, so no prompt and no tool has to guess which shape it is holding. No `source:` here on purpose. The prefetch block below reads the carrier's fact and this variable receives it, which leaves the per-route refusal for a variable naming a source its target cannot supply exactly as strict as it is: on a route with no caller ID the entry skips and this holds its default. Offered to the caller for a yes, never acted on unasked. Somebody may be ringing from a friend's phone, or may hold a second account, so until the verification step has heard them agree this value satisfies no `requires:` guard and appears in no prompt but that step's own. An earlier version of this note said no prompt ever reads the number back. That is now false rather than merely out of date: reading it back is the whole saving, and it replaced twelve spoken digits with one yes. The read-back turn does not cache, and that trade was made deliberately.
    )


# --- job metadata ------------------------------------------------------------
def _livekit_job_metadata(raw: str) -> dict:
    if not raw:
        return {}
    try:
        metadata = json.loads(raw)
    except json.JSONDecodeError as exc:
        raise RuntimeError("LiveKit job metadata must be valid JSON") from exc
    if not isinstance(metadata, dict):
        raise RuntimeError("LiveKit job metadata must be a JSON object")
    return metadata


# --- dispatched input variables ----------------------------------------------
def _dispatched_call_start(metadata: dict | None = None) -> dict:
    """Input variables arrive with the dispatch: the job metadata in production,
    or UNMUTE_CALL_START for a local `unmute dev --var` session."""
    values = dict((metadata or {}).get("call_start", {}))
    raw = os.getenv("UNMUTE_CALL_START")
    if raw:
        try:
            supplied = json.loads(raw)
        except json.JSONDecodeError as exc:
            raise RuntimeError("UNMUTE_CALL_START must be valid JSON") from exc
        if not isinstance(supplied, dict):
            raise RuntimeError("UNMUTE_CALL_START must be a JSON object")
        # The dispatch wins: env is the local stand-in for it.
        for name, value in supplied.items():
            values.setdefault(name, value)
    missing = []
    if "appointments" in values:
        value = values["appointments"]
        if not (isinstance(value, str)):
            raise RuntimeError("call_start.appointments must be string")
    if "booking_date" in values:
        value = values["booking_date"]
        if not (isinstance(value, str)):
            raise RuntimeError("call_start.booking_date must be string")
    if "caller_reason" in values:
        value = values["caller_reason"]
        if not (isinstance(value, str)):
            raise RuntimeError("call_start.caller_reason must be string")
    if "complaints" in values:
        value = values["complaints"]
        if not (isinstance(value, str)):
            raise RuntimeError("call_start.complaints must be string")
    if "customer" in values:
        value = values["customer"]
        if not (isinstance(value, str)):
            raise RuntimeError("call_start.customer must be string")
    if "customer_name" in values:
        value = values["customer_name"]
        if not (isinstance(value, str)):
            raise RuntimeError("call_start.customer_name must be string")
    if "customer_phone" in values:
        value = values["customer_phone"]
        if not (isinstance(value, str)):
            raise RuntimeError("call_start.customer_phone must be string")
    if missing:
        raise RuntimeError("Missing call_start fields: " + ", ".join(missing))
    return values


def _hydrate_call_start(userdata, values: dict) -> None:
    if "appointments" in values:
        userdata.appointments = values["appointments"]
    if "booking_date" in values:
        userdata.booking_date = values["booking_date"]
    if "caller_reason" in values:
        userdata.caller_reason = values["caller_reason"]
    if "complaints" in values:
        userdata.complaints = values["complaints"]
    if "customer" in values:
        userdata.customer = values["customer"]
    if "customer_name" in values:
        userdata.customer_name = values["customer_name"]
    if "customer_phone" in values:
        userdata.customer_phone = values["customer_phone"]
    return None


# --- LiveKit SIP call context -----------------------------------------------
# UNVERIFIED: Recheck these LiveKit-specific APIs with the LiveKit docs MCP.
# They were verified against docs.livekit.io on 2026-07-20 because MCP was not
# available during generation.
def _livekit_call_context(room_name: str, participant, metadata: dict) -> dict:
    expected_direction = "outbound" if "phone_number" in metadata else "inbound"
    direction = metadata.get("direction") or expected_direction
    if direction != expected_direction:
        raise RuntimeError("call direction does not match the LiveKit job")
    attributes = participant.attributes if participant is not None else {}
    remote_number = attributes.get("sip.phoneNumber") or metadata.get("phone_number")
    trunk_number = attributes.get("sip.trunkPhoneNumber")
    return {
        "session_id": room_name,
        "carrier": "twilio",
        "connection": "twilio_sip",
        "call_id": attributes.get("sip.callID") or metadata.get("call_id"),
        "direction": direction,
        "from_number": trunk_number if direction == "outbound" else remote_number,
        "to_number": remote_number if direction == "outbound" else trunk_number,
    }


def _hydrate_livekit_context(
    userdata: Userdata, context: dict, *, require_all: bool = True
) -> None:
    missing = []
    if missing and require_all:
        raise RuntimeError("Missing LiveKit SIP context fields: " + ", ".join(missing))


async def _livekit_entry_greeting(session: AgentSession) -> None:
    await session.say(
        "Hi, you've reached Sage and Stone. Robin speaking, what can I do for you?"
    )


class _TaskTransfer(Exception):
    """Internal signal that a task handed the caller to another agent."""

    def __init__(self, agent: Agent) -> None:
        super().__init__()
        self.agent = agent


# --- agents ----------------------------------------------------------------


class ComplaintSpecialist(Agent):
    def __init__(self, chat_ctx: NotGivenOr[llm.ChatContext] = NOT_GIVEN) -> None:
        super().__init__(
            instructions=COMPLAINT_SPECIALIST_PROMPT,
            chat_ctx=chat_ctx,
        )

    async def _refresh_prompt(self) -> None:
        """Render this agent's prompt again, after a step wrote declared state.

        on_enter runs once per entry and this agent is entered once per call, so
        without this the conversation state block keeps whatever it held before
        the first step finished. Its own steps looked right the whole time,
        because a step is entered per visit and renders on the way in.
        """
        await self.update_instructions(
            _render(COMPLAINT_SPECIALIST_PROMPT, self.session.userdata)
        )

    async def on_enter(self) -> None:
        # Rendered on the way in, and again by _refresh_prompt whenever a step
        # writes declared state: a call variable never changes, a declared value
        # does, and both reach this prompt through the same placeholders.
        await self.update_instructions(
            _render(COMPLAINT_SPECIALIST_PROMPT, self.session.userdata)
        )
        # This agent took over via handoff; let its own instructions drive the
        # opening (the prompt already says not to re-greet).
        # This opening turn withholds the agent's own handoffs (B3: an agent that can
        # hand the call back before it has said anything ping-pongs). The framework's
        # on-enter tool flag would hide them for the rest of the call instead, because
        # its filter follows the context of everything this reply starts: a live call
        # offered one specialist nothing but its delegate for ten turns while the
        # caller asked for another one (B: salon handoffs, 2026-08-20).
        self.session.generate_reply(
            tools=[t.id for t in self.tools if t.id not in {"to_concierge"}]
        )

    @function_tool
    async def look_up_refund_policy(
        self,
        ctx: RunContext,
        query: Annotated[
            str, Field(description="What to look up. Use the caller's own words.")
        ],
    ) -> dict:
        """Look up the salon's refund and complaints policy. Use this before you state any refund, redo, timescale, or goodwill offer, so you quote the policy instead of guessing it. It covers the redo window, the three refund tiers, colour corrections, retail returns, and how a complaint is handled.

        The results may not answer the question. They are ordered best first, and each carries a relevance score where a higher number means a closer match. If nothing returned actually answers what was asked, say you do not have that information rather than offering the closest result."""
        return await knowledge.look_up("refunds", query)

    @function_tool
    async def to_concierge(self, ctx: RunContext):
        """The caller changes to a request owned by another specialist."""
        return Concierge(
            chat_ctx=llm.ChatContext(
                items=[
                    m
                    for m in self.chat_ctx.messages()
                    if m.role in ("user", "assistant")
                ]
            )
        )

    @function_tool(on_duplicate="reject")
    async def to_manager(self, ctx: RunContext) -> str | None:
        """The caller explicitly asks for a manager or is clearly and strongly frustrated."""
        # One line per firing, so a live test can see which control the model
        # picked and when — the transfer that ran is otherwise invisible in the
        # logs until the carrier request goes out.
        #
        # This tool returns a string only on the paths where the caller is still
        # here and the conversation continues. A function tool's return value is
        # fed back to the LLM, which then takes another turn: harmless while the
        # caller is listening, wrong once the session is over. After a warm merge
        # that turn would speak into a room holding the caller and the person we
        # just handed them to, right after saying goodbye.
        logger.info("human transfer fired: to_manager (cold)")
        # cold: SIP REFER through the job context. The caller leaves the room.
        job_ctx = get_job_context()
        # Pick the SIP caller by kind: identities are assigned at dispatch, and a
        # room can hold more than one participant.
        caller = next(
            (
                participant
                for participant in job_ctx.room.remote_participants.values()
                if participant.kind == rtc.ParticipantKind.PARTICIPANT_KIND_SIP
            ),
            None,
        )
        if caller is None:
            # No SIP leg in the room, so there is nothing to refer. The usual
            # cause is a session that never arrived by phone: an Agent Console
            # run, a browser session, or a dispatch rule pointing at a different
            # agent. Cold acts on the caller's own leg, so unlike warm it cannot
            # be tested without a real inbound call.
            #
            # This branch logged nothing until 2026-08-12. On a live test that
            # day it fired, spoke a vague reply, and left an operator reading a
            # log that showed the tool firing and then simply stopping. The
            # earlier reasoning was that line 1 plus the absence of line 2 says
            # this on its own; absence is only a signal to somebody who already
            # knows to look for it.
            logger.info("cold transfer skipped: no phone caller in the room")
            return (
                "There is no phone caller to transfer, because this session did not "
                "arrive by phone. Tell the caller you cannot put them through and "
                "keep helping them."
            )
        # The tool speaks the announcement itself (V4/B4): leaving it to the
        # prompt lets a model say "putting you through" and never call this,
        # which a caller cannot tell apart from a transfer that failed.
        await ctx.session.say(
            "Putting you through now, one moment.", allow_interruptions=False
        )
        request = api.TransferSIPParticipantRequest(
            room_name=job_ctx.room.name,
            participant_identity=caller.identity,
            transfer_to=_refer_uri(os.environ["MANAGER_PHONE_NUMBER"]),
            play_dialtone=True,
        )
        request.ringing_timeout.FromNanoseconds(int(30 * 1e9))
        # The REFER goes out here. Logged before the call, so a request that never
        # returns is still visible as a phase the transfer reached.
        logger.info("cold transfer referring the caller out")
        started = time.monotonic()
        try:
            await job_ctx.api.sip.transfer_sip_participant(request)
        except Exception as error:
            # on_unavailable: hangup. Attempt the goodbye here, and always shut
            # down even if generating it fails or is cancelled.
            logger.info(
                "cold transfer failed after %ds: %s",
                int(time.monotonic() - started),
                error,
            )
            try:
                await ctx.session.generate_reply(
                    instructions="Tell the caller the transfer failed and say goodbye."
                )
            finally:
                ctx.session.shutdown()
            return None
        # A completed REFER takes the caller out of the room, which ends this
        # session on its own. Returning a value here would buy one LLM turn
        # spoken to nobody, racing the teardown.
        logger.info(
            "cold transfer completed after %ds", int(time.monotonic() - started)
        )
        return None

    @function_tool
    async def verify_customer(self, ctx: RunContext) -> dict:
        """Confirm who the caller is before any specialist handoff. The task reads the phone number back and needs a yes before it looks anyone up. When this flow finishes it returns its result to you. That result is the final outcome for this request: relay it to the caller and continue. Do not run this flow again for the same request. A result carrying `unserved_request` means a step could not serve that request and handed it back. The caller is still owed it: after one short line about the result, act on that request in the same turn, with your own tools, a handoff, or the same flow again. It is a new request, so running the flow for it is not running it again for the one that just finished. Never end the turn without acting on it, and never tell the caller you cannot."""
        # N13: snapshot before the task, restore after. An awaited AgentTask
        # merges its own turns into this agent's context when it returns
        # (livekit/agents/voice/agent.py, merge on handoff-return), so without
        # this the follow-up prompt ends on the task's last assistant line with
        # no tool record of the work. That reads as unfinished, and the model
        # runs the same flow again (B: multi-task delegated twice and booked
        # nothing, 2026-08-15). The task-group branch below always did this.
        owner_ctx = self.chat_ctx.copy()
        result = await VerifyCustomer()  # history: reset — the task starts fresh
        await self.update_chat_ctx(owner_ctx)
        ctx.userdata.customer = result["customer"]
        ctx.userdata.customer_phone = result["customer_phone"]
        # The step that confirms this value has just assigned it, so it is settled
        # now and the guard stops holding it back. Discarding here rather than in
        # the step's prompt is the point: a model can be talked out of an
        # instruction, not out of a set membership test.
        #
        # getattr with a default, matching the guard: the set is created by the
        # pre-fetch, and this step can be reached on a path where the pre-fetch
        # never ran. A bare attribute read there is an AttributeError inside a
        # finish handler, which is a call that dies mid-step.
        getattr(ctx.userdata, "_unconfirmed", set()).discard("customer_phone")
        # The values above are in this agent's own prompt, which was rendered on
        # entry and would otherwise still read as it did before this step ran.
        await self._refresh_prompt()
        # C4/N13: the task's turns are not propagated back; the typed result is
        # the only return.
        return result

    @function_tool
    async def handle_complaint(self, ctx: RunContext) -> dict:
        """The verified caller is unhappy about something and wants it recorded. When this flow finishes it returns its result to you. That result is the final outcome for this request: relay it to the caller and continue. Do not run this flow again for the same request. A result carrying `unserved_request` means a step could not serve that request and handed it back. The caller is still owed it: after one short line about the result, act on that request in the same turn, with your own tools, a handoff, or the same flow again. It is a new request, so running the flow for it is not running it again for the one that just finished. Never end the turn without acting on it, and never tell the caller you cannot. Before this can run you need: customer.status."""
        _unmet = _unmet_prerequisites(ctx.userdata, ["customer.status"])
        if _unmet:
            # requires guard (machine-checked): the step does not start, and the
            # refusal goes to the model rather than to the caller.
            _tries = _prerequisite_refusals.get("handle_complaint", 0) + 1
            _prerequisite_refusals["handle_complaint"] = _tries
            _at_limit = _tries >= _PREREQUISITE_LIMIT
            if _at_limit:
                logger.info(
                    "prerequisite guard: step %s refused, unmet %s, retry limit reached; asking the caller",
                    "handle_complaint",
                    ", ".join(_unmet),
                )
            else:
                logger.info(
                    "prerequisite guard: step %s refused, unmet %s",
                    "handle_complaint",
                    ", ".join(_unmet),
                )
            return {"refused": _prerequisite_refusal(_unmet, _at_limit)}
        _prerequisite_refusals["handle_complaint"] = 0

        # N13: snapshot before the task, restore after. An awaited AgentTask
        # merges its own turns into this agent's context when it returns
        # (livekit/agents/voice/agent.py, merge on handoff-return), so without
        # this the follow-up prompt ends on the task's last assistant line with
        # no tool record of the work. That reads as unfinished, and the model
        # runs the same flow again (B: multi-task delegated twice and booked
        # nothing, 2026-08-15). The task-group branch below always did this.
        owner_ctx = self.chat_ctx.copy()
        result = await HandleComplaint(
            chat_ctx=llm.ChatContext(
                items=[
                    m
                    for m in self.chat_ctx.messages()
                    if m.role in ("user", "assistant")
                ]
            )
        )
        await self.update_chat_ctx(owner_ctx)
        # An entry added, not the value replaced: only the step producing it
        # knows whether the caller added an intent or swapped one, and the list
        # starts empty so this never has to create it. The helper drops an
        # absent entry and a structured one already on the list, so neither a
        # step that concluded nothing nor a step re-entered mid-call grows it.
        _append_entry(ctx.userdata.caller_reason, result["reason"])
        # An entry added, not the value replaced: only the step producing it
        # knows whether the caller added an intent or swapped one, and the list
        # starts empty so this never has to create it. The helper drops an
        # absent entry and a structured one already on the list, so neither a
        # step that concluded nothing nor a step re-entered mid-call grows it.
        _append_entry(ctx.userdata.complaints, result["complaint"])
        # The values above are in this agent's own prompt, which was rendered on
        # entry and would otherwise still read as it did before this step ran.
        await self._refresh_prompt()
        # C4/N13: the task's turns are not propagated back; the typed result is
        # the only return.
        return result


class Concierge(Agent):
    def __init__(
        self, chat_ctx: NotGivenOr[llm.ChatContext] = NOT_GIVEN, initial: bool = False
    ) -> None:
        self._initial = initial
        super().__init__(
            instructions=CONCIERGE_PROMPT,
            chat_ctx=chat_ctx,
        )

    async def _refresh_prompt(self) -> None:
        """Render this agent's prompt again, after a step wrote declared state.

        on_enter runs once per entry and this agent is entered once per call, so
        without this the conversation state block keeps whatever it held before
        the first step finished. Its own steps looked right the whole time,
        because a step is entered per visit and renders on the way in.
        """
        await self.update_instructions(_render(CONCIERGE_PROMPT, self.session.userdata))

    async def on_enter(self) -> None:
        # Rendered on the way in, and again by _refresh_prompt whenever a step
        # writes declared state: a call variable never changes, a declared value
        # does, and both reach this prompt through the same placeholders.
        await self.update_instructions(_render(CONCIERGE_PROMPT, self.session.userdata))
        if not self._initial:
            # This opening turn withholds the agent's own handoffs (B3: an agent that can
            # hand the call back before it has said anything ping-pongs). The framework's
            # on-enter tool flag would hide them for the rest of the call instead, because
            # its filter follows the context of everything this reply starts: a live call
            # offered one specialist nothing but its delegate for ten turns while the
            # caller asked for another one (B: salon handoffs, 2026-08-20).
            self.session.generate_reply(
                tools=[t.id for t in self.tools if t.id not in {"to_complaints"}]
            )
            return
        pass  # the caller speaks first

    @function_tool
    async def look_up_salon_info(
        self,
        ctx: RunContext,
        query: Annotated[
            str, Field(description="What to look up. Use the caller's own words.")
        ],
    ) -> dict:
        """Look up anything about the salon itself: services and prices, how long things take, opening hours, the stylists and what each one does, booking, deposits, cancellation and lateness terms, payment methods, parking, and accessibility. Use this whenever the caller asks a question about how the salon works, not only about price. Quote the document rather than estimating.
        Prefer this over asking the caller for anything. A question about the salon is not a booking request: look it up and answer it, and only start verification if the caller actually wants to make, move, or cancel an appointment.

        The results may not answer the question. They are ordered best first, and each carries a relevance score where a higher number means a closer match. If nothing returned actually answers what was asked, say you do not have that information rather than offering the closest result."""
        return await knowledge.look_up("services", query)

    @function_tool
    async def to_complaints(self, ctx: RunContext):
        """The verified caller has a complaint or service problem."""
        return ComplaintSpecialist(
            chat_ctx=llm.ChatContext(
                items=[
                    m
                    for m in self.chat_ctx.messages()
                    if m.role in ("user", "assistant")
                ]
            )
        )

    @function_tool(on_duplicate="reject")
    async def to_manager(self, ctx: RunContext) -> str | None:
        """The caller explicitly asks for a manager or is clearly and strongly frustrated."""
        # One line per firing, so a live test can see which control the model
        # picked and when — the transfer that ran is otherwise invisible in the
        # logs until the carrier request goes out.
        #
        # This tool returns a string only on the paths where the caller is still
        # here and the conversation continues. A function tool's return value is
        # fed back to the LLM, which then takes another turn: harmless while the
        # caller is listening, wrong once the session is over. After a warm merge
        # that turn would speak into a room holding the caller and the person we
        # just handed them to, right after saying goodbye.
        logger.info("human transfer fired: to_manager (cold)")
        # cold: SIP REFER through the job context. The caller leaves the room.
        job_ctx = get_job_context()
        # Pick the SIP caller by kind: identities are assigned at dispatch, and a
        # room can hold more than one participant.
        caller = next(
            (
                participant
                for participant in job_ctx.room.remote_participants.values()
                if participant.kind == rtc.ParticipantKind.PARTICIPANT_KIND_SIP
            ),
            None,
        )
        if caller is None:
            # No SIP leg in the room, so there is nothing to refer. The usual
            # cause is a session that never arrived by phone: an Agent Console
            # run, a browser session, or a dispatch rule pointing at a different
            # agent. Cold acts on the caller's own leg, so unlike warm it cannot
            # be tested without a real inbound call.
            #
            # This branch logged nothing until 2026-08-12. On a live test that
            # day it fired, spoke a vague reply, and left an operator reading a
            # log that showed the tool firing and then simply stopping. The
            # earlier reasoning was that line 1 plus the absence of line 2 says
            # this on its own; absence is only a signal to somebody who already
            # knows to look for it.
            logger.info("cold transfer skipped: no phone caller in the room")
            return (
                "There is no phone caller to transfer, because this session did not "
                "arrive by phone. Tell the caller you cannot put them through and "
                "keep helping them."
            )
        # The tool speaks the announcement itself (V4/B4): leaving it to the
        # prompt lets a model say "putting you through" and never call this,
        # which a caller cannot tell apart from a transfer that failed.
        await ctx.session.say(
            "Putting you through now, one moment.", allow_interruptions=False
        )
        request = api.TransferSIPParticipantRequest(
            room_name=job_ctx.room.name,
            participant_identity=caller.identity,
            transfer_to=_refer_uri(os.environ["MANAGER_PHONE_NUMBER"]),
            play_dialtone=True,
        )
        request.ringing_timeout.FromNanoseconds(int(30 * 1e9))
        # The REFER goes out here. Logged before the call, so a request that never
        # returns is still visible as a phase the transfer reached.
        logger.info("cold transfer referring the caller out")
        started = time.monotonic()
        try:
            await job_ctx.api.sip.transfer_sip_participant(request)
        except Exception as error:
            # on_unavailable: hangup. Attempt the goodbye here, and always shut
            # down even if generating it fails or is cancelled.
            logger.info(
                "cold transfer failed after %ds: %s",
                int(time.monotonic() - started),
                error,
            )
            try:
                await ctx.session.generate_reply(
                    instructions="Tell the caller the transfer failed and say goodbye."
                )
            finally:
                ctx.session.shutdown()
            return None
        # A completed REFER takes the caller out of the room, which ends this
        # session on its own. Returning a value here would buy one LLM turn
        # spoken to nobody, racing the teardown.
        logger.info(
            "cold transfer completed after %ds", int(time.monotonic() - started)
        )
        return None

    @function_tool
    async def verify_customer(self, ctx: RunContext) -> dict:
        """Confirm who the caller is before any specialist handoff. The task reads the phone number back and needs a yes before it looks anyone up. When this flow finishes it returns its result to you. That result is the final outcome for this request: relay it to the caller and continue. Do not run this flow again for the same request. A result carrying `unserved_request` means a step could not serve that request and handed it back. The caller is still owed it: after one short line about the result, act on that request in the same turn, with your own tools, a handoff, or the same flow again. It is a new request, so running the flow for it is not running it again for the one that just finished. Never end the turn without acting on it, and never tell the caller you cannot."""
        # N13: snapshot before the task, restore after. An awaited AgentTask
        # merges its own turns into this agent's context when it returns
        # (livekit/agents/voice/agent.py, merge on handoff-return), so without
        # this the follow-up prompt ends on the task's last assistant line with
        # no tool record of the work. That reads as unfinished, and the model
        # runs the same flow again (B: multi-task delegated twice and booked
        # nothing, 2026-08-15). The task-group branch below always did this.
        owner_ctx = self.chat_ctx.copy()
        result = await VerifyCustomer()  # history: reset — the task starts fresh
        await self.update_chat_ctx(owner_ctx)
        ctx.userdata.customer = result["customer"]
        ctx.userdata.customer_phone = result["customer_phone"]
        # The step that confirms this value has just assigned it, so it is settled
        # now and the guard stops holding it back. Discarding here rather than in
        # the step's prompt is the point: a model can be talked out of an
        # instruction, not out of a set membership test.
        #
        # getattr with a default, matching the guard: the set is created by the
        # pre-fetch, and this step can be reached on a path where the pre-fetch
        # never ran. A bare attribute read there is an AttributeError inside a
        # finish handler, which is a call that dies mid-step.
        getattr(ctx.userdata, "_unconfirmed", set()).discard("customer_phone")
        # The values above are in this agent's own prompt, which was rendered on
        # entry and would otherwise still read as it did before this step ran.
        await self._refresh_prompt()
        # C4/N13: the task's turns are not propagated back; the typed result is
        # the only return.
        return result

    @function_tool
    async def manage_booking(self, ctx: RunContext) -> dict | Agent:
        """The caller wants to create, modify, or cancel a booking. When this flow finishes it returns its result to you. That result is the final outcome for this request: relay it to the caller and continue. Do not run this flow again for the same request. A result carrying `unserved_request` means a step could not serve that request and handed it back. The caller is still owed it: after one short line about the result, act on that request in the same turn, with your own tools, a handoff, or the same flow again. It is a new request, so running the flow for it is not running it again for the one that just finished. Never end the turn without acting on it, and never tell the caller you cannot. Before this can run you need: customer_phone (call verify_customer to get it), customer.status."""
        _unmet = _unmet_prerequisites(
            ctx.userdata, ["customer_phone", "customer.status"]
        )
        if _unmet:
            # requires guard (machine-checked): the step does not start, and the
            # refusal goes to the model rather than to the caller.
            _tries = _prerequisite_refusals.get("manage_booking", 0) + 1
            _prerequisite_refusals["manage_booking"] = _tries
            _at_limit = _tries >= _PREREQUISITE_LIMIT
            if _at_limit:
                logger.info(
                    "prerequisite guard: step %s refused, unmet %s, retry limit reached; asking the caller",
                    "manage_booking",
                    ", ".join(_unmet),
                )
            else:
                logger.info(
                    "prerequisite guard: step %s refused, unmet %s",
                    "manage_booking",
                    ", ".join(_unmet),
                )
            return {"refused": _prerequisite_refusal(_unmet, _at_limit)}
        _prerequisite_refusals["manage_booking"] = 0

        # N13: snapshot before the task, restore after. An awaited AgentTask
        # merges its own turns into this agent's context when it returns
        # (livekit/agents/voice/agent.py, merge on handoff-return), so without
        # this the follow-up prompt ends on the task's last assistant line with
        # no tool record of the work. That reads as unfinished, and the model
        # runs the same flow again (B: multi-task delegated twice and booked
        # nothing, 2026-08-15). The task-group branch below always did this.
        owner_ctx = self.chat_ctx.copy()
        try:
            result = await ManageBooking(
                chat_ctx=llm.ChatContext(
                    items=[
                        m
                        for m in self.chat_ctx.messages()
                        if m.role in ("user", "assistant")
                    ]
                )
            )
        except _TaskTransfer as transfer:
            return transfer.agent
        await self.update_chat_ctx(owner_ctx)
        # An entry added, not the value replaced: only the step producing it
        # knows whether the caller added an intent or swapped one, and the list
        # starts empty so this never has to create it. The helper drops an
        # absent entry and a structured one already on the list, so neither a
        # step that concluded nothing nor a step re-entered mid-call grows it.
        _append_entry(ctx.userdata.appointments, result["appointment"])
        # An entry added, not the value replaced: only the step producing it
        # knows whether the caller added an intent or swapped one, and the list
        # starts empty so this never has to create it. The helper drops an
        # absent entry and a structured one already on the list, so neither a
        # step that concluded nothing nor a step re-entered mid-call grows it.
        _append_entry(ctx.userdata.caller_reason, result["reason"])
        # The values above are in this agent's own prompt, which was rendered on
        # entry and would otherwise still read as it did before this step ran.
        await self._refresh_prompt()
        # C4/N13: the task's turns are not propagated back; the typed result is
        # the only return.
        return result


# --- tasks -----------------------------------------------------------------
def _task_result(values: dict, unserved_request: str) -> dict:
    """A step that could not serve a request names it on the way out, so the
    agent that owns the step reads it off the result and takes it from there."""
    if not unserved_request:
        return values
    return {**values, "unserved_request": unserved_request}


class _RetryEmptyTaskResponseMixin:
    _response_tool_call_ids: set[str]

    async def llm_node(self, chat_ctx, tools, model_settings):
        # Every generated user of this mixin is an AgentTask; narrow that
        # invariant here so the emitted project type-checks without a new base.
        assert isinstance(self, Agent)
        # ponytail: keyed on the SDK's own marker rather than the placeholder
        # wording, and the emitted-code test is what catches it changing.
        #
        # The delegate that started this task is still running, so the framework
        # injects that call plus a placeholder output reading "The tool call is
        # still in progress." into the context (voice/generation.py,
        # _inject_running_tool_calls). For the agent that made the call that is
        # right: it stops the model re-issuing a call already in flight. A task
        # is a different agent with a different prompt, and its opening turn
        # reads an unfinished tool call it never made: it then answers with
        # nothing, or apologises for a failure that did not happen, which the
        # caller hears as the agent breaking. Reproduced on 3 of 3 scripted
        # salon calls (B: task opened silent after delegate, 2026-08-21).
        running_placeholders = {
            item.call_id
            for item in chat_ctx.items
            if isinstance(item, llm.FunctionCall)
            and item.extra.get("__lk_running_placeholder__")
        }
        if running_placeholders:
            chat_ctx = chat_ctx.copy()
            chat_ctx.items = [
                item
                for item in chat_ctx.items
                if getattr(item, "call_id", None) not in running_placeholders
            ]
        completed_tool_call_ids = {
            item.call_id
            for item in chat_ctx.items
            if isinstance(item, llm.FunctionCallOutput)
            and item.call_id in self._response_tool_call_ids
        }
        post_tool = bool(completed_tool_call_ids)
        finish_tool = llm.ToolContext(tools).get_function_tool("finish")
        if finish_tool is None:
            raise RuntimeError("task retry has no finish tool")
        finish_only = False
        request_tools: list[llm.Tool] = tools
        request_chat_ctx = chat_ctx
        for attempt in range(3):
            has_response = False
            async for chunk in Agent.default.llm_node(
                self, request_chat_ctx, request_tools, model_settings
            ):
                if isinstance(chunk, str):
                    has_response = has_response or bool(chunk.strip())
                elif isinstance(chunk, llm.ChatChunk) and chunk.delta is not None:
                    delta = chunk.delta
                    tool_calls = delta.tool_calls or []
                    if finish_only and tool_calls:
                        allowed = [call for call in tool_calls if call.name == "finish"]
                        if len(allowed) != len(tool_calls):
                            logger.warning(
                                "task post-tool reply tried another non-finish tool; "
                                "ignoring it"
                            )
                            chunk = chunk.model_copy(
                                update={
                                    "delta": delta.model_copy(
                                        update={"tool_calls": allowed}
                                    )
                                }
                            )
                            delta = chunk.delta
                            if delta is None:
                                raise RuntimeError("task retry lost its response delta")
                            tool_calls = allowed
                    self._response_tool_call_ids.update(
                        call.call_id for call in tool_calls
                    )
                    has_response = has_response or bool(
                        tool_calls or (delta.content or "").strip()
                    )
                yield chunk
            if has_response:
                self._response_tool_call_ids.difference_update(completed_tool_call_ids)
                return
            if attempt < 2:
                logger.warning("task response was empty; retrying %d/2", attempt + 1)

                if attempt == 0 and post_tool:
                    finish_only = True
                    request_tools = [finish_tool]

                # Copy the original each time. Responses API reuses a previous
                # response when the context is unchanged, so each retry needs
                # a distinct instruction as well as a fresh context object.
                request_chat_ctx = chat_ctx.copy()
                instructions_index = request_chat_ctx.index_by_id(
                    "lk.agent_task.instructions"
                )
                if instructions_index is None:
                    raise RuntimeError("task retry has no instruction message")
                instructions = request_chat_ctx.items[instructions_index]
                if not isinstance(instructions, llm.ChatMessage):
                    raise RuntimeError("task retry instruction has an invalid type")
                instruction_text = instructions.raw_text_content
                if not instruction_text:
                    raise RuntimeError("task retry instruction is empty")
                if finish_only:
                    recovery = (
                        "The response after the tool result was empty. Use the tool "
                        "result already in context. Do not repeat that operation or "
                        "call another operation. Produce the task's next valid "
                        "response now, using what the caller has already told you "
                        "rather than asking again; call finish only if the task is "
                        "complete."
                        if attempt == 0
                        else "The finish retry was also empty. This is the second "
                        "retry. Use the existing tool result without repeating any "
                        "operation. Produce the task's next valid response now; call "
                        "finish only if the task is complete."
                    )
                else:
                    # "Do not ask again" is the load-bearing half.
                    #
                    # Without it a retry points the model at the task instructions
                    # and it restarts the script: observed on a live call, the
                    # caller answered "tomorrow if possible", the response came
                    # back empty, and the retry asked "what day would you like?"
                    # again. The caller's turn is in the copied context the whole
                    # time; the instruction just has to say to use it.
                    recovery = (
                        "The previous response was empty. Follow the current task "
                        "instructions and produce its next valid response now. The "
                        "caller's turns are already in context: use what they have "
                        "already told you, and never ask again for something they "
                        "have answered."
                        if attempt == 0
                        else "The prior retry was also empty. This is the second "
                        "retry. Follow the current task instructions and produce "
                        "its next non-empty valid response now, using what the "
                        "caller has already said rather than asking again."
                    )
                request_chat_ctx.items[instructions_index] = instructions.model_copy(
                    update={"content": [instruction_text + "\n\n" + recovery]}
                )

        logger.warning("task response stayed empty after two retries")
        if finish_only:
            yield (
                "I couldn't finish after the last tool result. Please ask me to "
                "check the current state before trying again."
            )
        else:
            yield "Sorry, I couldn't complete that. Please try again."


class HandleComplaint(_RetryEmptyTaskResponseMixin, AgentTask[dict]):
    def __init__(self, chat_ctx: NotGivenOr[llm.ChatContext] = NOT_GIVEN) -> None:
        super().__init__(instructions=HANDLE_COMPLAINT_PROMPT, chat_ctx=chat_ctx)
        self._response_tool_call_ids: set[str] = set()

    async def on_enter(self) -> None:
        await self.update_instructions(
            _render(HANDLE_COMPLAINT_PROMPT, self.session.userdata)
        )
        # The task's own instructions describe this step; let them drive the opening.
        self.session.generate_reply()

    @function_tool
    async def record_complaint(
        self,
        ctx: RunContext,
        requested_resolution: Annotated[
            str,
            Field(
                description="What the customer wants the salon to do, or an empty string"
            ),
        ],
        summary: Annotated[
            str, Field(description="Short factual summary of the complaint")
        ],
    ) -> dict:
        """Record the verified customer's complaint after enough facts are known. This does not replace a requested manager transfer."""
        refusal = _refusal(
            "record_complaint",
            ctx.userdata,
            [
                (
                    "customer_phone",
                    "The caller's phone number in E.164: a plus sign, then digits, with no spaces, brackets or dashes. One shape for every phone number in this package, the MANAGER_PHONE_NUMBER transfer destination included, so no prompt and no tool has to guess which shape it is holding.\nNo `source:` here on purpose. The prefetch block below reads the carrier's fact and this variable receives it, which leaves the per-route refusal for a variable naming a source its target cannot supply exactly as strict as it is: on a route with no caller ID the entry skips and this holds its default.\nOffered to the caller for a yes, never acted on unasked. Somebody may be ringing from a friend's phone, or may hold a second account, so until the verification step has heard them agree this value satisfies no `requires:` guard and appears in no prompt but that step's own.\nAn earlier version of this note said no prompt ever reads the number back. That is now false rather than merely out of date: reading it back is the whole saving, and it replaced twelve spoken digits with one yes. The read-back turn does not cache, and that trade was made deliberately.",
                )
            ],
        )
        if refusal:
            return {"refused": refusal}
        # Not awaited, on purpose: see the same line in webhook_tool.
        self.session.say("Noting that down.")
        result = tools.record_complaint.record_complaint(
            requested_resolution=requested_resolution,
            summary=summary,
            customer_phone=ctx.userdata.customer_phone,
        )
        if inspect.isawaitable(result):
            result = await result
        return result

    @function_tool
    async def finish(
        self,
        ctx: RunContext,
        complaint: Complaint,
        reason: Literal[
            "create_booking",
            "modify_booking",
            "cancel_booking",
            "request_informations",
            "complain",
        ],
        summary: str,
        unserved_request: Annotated[
            str,
            Field(
                description="Leave empty unless the caller asked for something this step cannot serve. Then put that request here in one short plain sentence, in the caller's own terms, so the agent that owns this step can take it."
            ),
        ] = "",
    ) -> str | None:
        """Record the result of this step and finish. complete() is the sole
        resolution; do not relay anything after it."""
        # Validated before anything is recorded, and before the terminal claim:
        # a value that does not fit its declared type never enters the state,
        # the previous contents stand, and the message goes back to the model so
        # it can correct itself on the next turn instead of the step recording
        # something wrong.
        try:
            _values = _typed_result(
                "handle_complaint",
                {"complaint": complaint, "reason": reason, "summary": summary},
            )
        except _StateRefused as refused:
            logger.warning("finish %s: %s", "handle_complaint", refused.message)
            return f"Not recorded: {refused.message}. Ask again, then call finish with a value that fits."
        self.complete(_task_result(_values, unserved_request))


class ManageBooking(_RetryEmptyTaskResponseMixin, AgentTask[dict]):
    def __init__(self, chat_ctx: NotGivenOr[llm.ChatContext] = NOT_GIVEN) -> None:
        super().__init__(instructions=MANAGE_BOOKING_PROMPT, chat_ctx=chat_ctx)
        self._response_tool_call_ids: set[str] = set()
        self._terminal_claimed = False

    def _claim_terminal(self) -> bool:
        if self._terminal_claimed:
            return False
        self._terminal_claimed = True
        return True

    async def on_enter(self) -> None:
        await self.update_instructions(
            _render(MANAGE_BOOKING_PROMPT, self.session.userdata)
        )
        # The task's own instructions describe this step; let them drive the opening.
        # This opening turn withholds the agent's own handoffs (B3: an agent that can
        # hand the call back before it has said anything ping-pongs). The framework's
        # on-enter tool flag would hide them for the rest of the call instead, because
        # its filter follows the context of everything this reply starts: a live call
        # offered one specialist nothing but its delegate for ten turns while the
        # caller asked for another one (B: salon handoffs, 2026-08-20).
        self.session.generate_reply(
            tools=[t.id for t in self.tools if t.id not in {"to_complaints"}]
        )

    @function_tool
    async def list_bookings(self, ctx: RunContext) -> dict:
        """List the verified customer's active bookings. The phone number comes from verification and is never supplied or guessed by the model."""
        refusal = _refusal(
            "list_bookings",
            ctx.userdata,
            [
                (
                    "customer_phone",
                    "The caller's phone number in E.164: a plus sign, then digits, with no spaces, brackets or dashes. One shape for every phone number in this package, the MANAGER_PHONE_NUMBER transfer destination included, so no prompt and no tool has to guess which shape it is holding.\nNo `source:` here on purpose. The prefetch block below reads the carrier's fact and this variable receives it, which leaves the per-route refusal for a variable naming a source its target cannot supply exactly as strict as it is: on a route with no caller ID the entry skips and this holds its default.\nOffered to the caller for a yes, never acted on unasked. Somebody may be ringing from a friend's phone, or may hold a second account, so until the verification step has heard them agree this value satisfies no `requires:` guard and appears in no prompt but that step's own.\nAn earlier version of this note said no prompt ever reads the number back. That is now false rather than merely out of date: reading it back is the whole saving, and it replaced twelve spoken digits with one yes. The read-back turn does not cache, and that trade was made deliberately.",
                )
            ],
        )
        if refusal:
            return {"refused": refusal}
        # Not awaited, on purpose: see the same line in webhook_tool.
        self.session.say("Let me look.")
        result = tools.list_bookings.list_bookings(
            customer_phone=ctx.userdata.customer_phone
        )
        if inspect.isawaitable(result):
            result = await result
        return result

    @function_tool
    async def check_availability(
        self,
        ctx: RunContext,
        date: Annotated[str, Field(description="Preferred date in YYYY-MM-DD form")],
        service: Literal["haircut", "hair-color", "blowout"],
    ) -> dict:
        """List currently open salon times for one supported service and date."""
        # Not awaited, on purpose: see the same line in webhook_tool.
        self.session.say("Let me check.")
        result = tools.check_availability.check_availability(date=date, service=service)
        if inspect.isawaitable(result):
            result = await result
        return result

    @function_tool
    async def create_booking(
        self,
        ctx: RunContext,
        confirmed: Annotated[
            bool,
            Field(
                description="Exact true value from the shared confirm_booking result"
            ),
        ],
        service: Literal["haircut", "hair-color", "blowout"],
        slot_id: Annotated[
            str, Field(description="Exact slot ID returned by check_availability")
        ],
    ) -> dict:
        """Create one confirmed booking for the verified customer using an exact slot returned by availability. Never invent a slot or a phone number."""
        refusal = _refusal(
            "create_booking",
            ctx.userdata,
            [
                (
                    "customer_phone",
                    "The caller's phone number in E.164: a plus sign, then digits, with no spaces, brackets or dashes. One shape for every phone number in this package, the MANAGER_PHONE_NUMBER transfer destination included, so no prompt and no tool has to guess which shape it is holding.\nNo `source:` here on purpose. The prefetch block below reads the carrier's fact and this variable receives it, which leaves the per-route refusal for a variable naming a source its target cannot supply exactly as strict as it is: on a route with no caller ID the entry skips and this holds its default.\nOffered to the caller for a yes, never acted on unasked. Somebody may be ringing from a friend's phone, or may hold a second account, so until the verification step has heard them agree this value satisfies no `requires:` guard and appears in no prompt but that step's own.\nAn earlier version of this note said no prompt ever reads the number back. That is now false rather than merely out of date: reading it back is the whole saving, and it replaced twelve spoken digits with one yes. The read-back turn does not cache, and that trade was made deliberately.",
                )
            ],
        )
        if refusal:
            return {"refused": refusal}
        # Not awaited, on purpose: see the same line in webhook_tool.
        self.session.say("Booking that in.")
        result = tools.create_booking.create_booking(
            confirmed=confirmed,
            service=service,
            slot_id=slot_id,
            customer_phone=ctx.userdata.customer_phone,
        )
        if inspect.isawaitable(result):
            result = await result
        return result

    @function_tool
    async def modify_booking(
        self,
        ctx: RunContext,
        booking_id: Annotated[
            str, Field(description="Exact booking ID returned by list_bookings")
        ],
        confirmed: Annotated[
            bool,
            Field(
                description="Exact true value from the shared confirm_booking result"
            ),
        ],
        service: Literal["haircut", "hair-color", "blowout"],
        slot_id: Annotated[
            str, Field(description="Exact slot ID returned by check_availability")
        ],
    ) -> dict:
        """Atomically move one confirmed active booking owned by the verified customer to an exact available slot."""
        refusal = _refusal(
            "modify_booking",
            ctx.userdata,
            [
                (
                    "customer_phone",
                    "The caller's phone number in E.164: a plus sign, then digits, with no spaces, brackets or dashes. One shape for every phone number in this package, the MANAGER_PHONE_NUMBER transfer destination included, so no prompt and no tool has to guess which shape it is holding.\nNo `source:` here on purpose. The prefetch block below reads the carrier's fact and this variable receives it, which leaves the per-route refusal for a variable naming a source its target cannot supply exactly as strict as it is: on a route with no caller ID the entry skips and this holds its default.\nOffered to the caller for a yes, never acted on unasked. Somebody may be ringing from a friend's phone, or may hold a second account, so until the verification step has heard them agree this value satisfies no `requires:` guard and appears in no prompt but that step's own.\nAn earlier version of this note said no prompt ever reads the number back. That is now false rather than merely out of date: reading it back is the whole saving, and it replaced twelve spoken digits with one yes. The read-back turn does not cache, and that trade was made deliberately.",
                )
            ],
        )
        if refusal:
            return {"refused": refusal}
        # Not awaited, on purpose: see the same line in webhook_tool.
        self.session.say("Moving that now.")
        result = tools.modify_booking.modify_booking(
            booking_id=booking_id,
            confirmed=confirmed,
            service=service,
            slot_id=slot_id,
            customer_phone=ctx.userdata.customer_phone,
        )
        if inspect.isawaitable(result):
            result = await result
        return result

    @function_tool
    async def cancel_booking(
        self,
        ctx: RunContext,
        booking_id: Annotated[
            str, Field(description="Exact booking ID returned by list_bookings")
        ],
        confirmed: Annotated[
            bool,
            Field(
                description="Exact true value from the shared confirm_booking result"
            ),
        ],
    ) -> dict:
        """Cancel one confirmed booking owned by the verified customer. Use an exact booking ID returned by list_bookings."""
        refusal = _refusal(
            "cancel_booking",
            ctx.userdata,
            [
                (
                    "customer_phone",
                    "The caller's phone number in E.164: a plus sign, then digits, with no spaces, brackets or dashes. One shape for every phone number in this package, the MANAGER_PHONE_NUMBER transfer destination included, so no prompt and no tool has to guess which shape it is holding.\nNo `source:` here on purpose. The prefetch block below reads the carrier's fact and this variable receives it, which leaves the per-route refusal for a variable naming a source its target cannot supply exactly as strict as it is: on a route with no caller ID the entry skips and this holds its default.\nOffered to the caller for a yes, never acted on unasked. Somebody may be ringing from a friend's phone, or may hold a second account, so until the verification step has heard them agree this value satisfies no `requires:` guard and appears in no prompt but that step's own.\nAn earlier version of this note said no prompt ever reads the number back. That is now false rather than merely out of date: reading it back is the whole saving, and it replaced twelve spoken digits with one yes. The read-back turn does not cache, and that trade was made deliberately.",
                )
            ],
        )
        if refusal:
            return {"refused": refusal}
        # Not awaited, on purpose: see the same line in webhook_tool.
        self.session.say("Cancelling that now.")
        result = tools.cancel_booking.cancel_booking(
            booking_id=booking_id,
            confirmed=confirmed,
            customer_phone=ctx.userdata.customer_phone,
        )
        if inspect.isawaitable(result):
            result = await result
        return result

    @function_tool
    async def to_complaints(self, ctx: RunContext):
        """The verified caller has a complaint or service problem."""
        if not self._claim_terminal():
            return
        try:
            self.complete(
                _TaskTransfer(
                    ComplaintSpecialist(
                        chat_ctx=llm.ChatContext(
                            items=[
                                m
                                for m in self.chat_ctx.messages()
                                if m.role in ("user", "assistant")
                            ]
                        )
                    )
                )
            )
        except BaseException:
            self._terminal_claimed = False
            raise

    @function_tool
    async def finish(
        self,
        ctx: RunContext,
        appointment: Appointment | None,
        reason: Literal[
            "create_booking",
            "modify_booking",
            "cancel_booking",
            "request_informations",
            "complain",
        ],
        summary: str,
        unserved_request: Annotated[
            str,
            Field(
                description="Leave empty unless the caller asked for something this step cannot serve. Then put that request here in one short plain sentence, in the caller's own terms, so the agent that owns this step can take it."
            ),
        ] = "",
    ) -> str | None:
        """Record the result of this step and finish. complete() is the sole
        resolution; do not relay anything after it."""
        # Validated before anything is recorded, and before the terminal claim:
        # a value that does not fit its declared type never enters the state,
        # the previous contents stand, and the message goes back to the model so
        # it can correct itself on the next turn instead of the step recording
        # something wrong.
        try:
            _values = _typed_result(
                "manage_booking",
                {"appointment": appointment, "reason": reason, "summary": summary},
            )
        except _StateRefused as refused:
            logger.warning("finish %s: %s", "manage_booking", refused.message)
            return f"Not recorded: {refused.message}. Ask again, then call finish with a value that fits."
        if not self._claim_terminal():
            return
        try:
            self.complete(_task_result(_values, unserved_request))
        except BaseException:
            self._terminal_claimed = False
            raise


class VerifyCustomer(_RetryEmptyTaskResponseMixin, AgentTask[dict]):
    def __init__(self, chat_ctx: NotGivenOr[llm.ChatContext] = NOT_GIVEN) -> None:
        super().__init__(instructions=VERIFY_CUSTOMER_PROMPT, chat_ctx=chat_ctx)
        self._response_tool_call_ids: set[str] = set()

    async def on_enter(self) -> None:
        await self.update_instructions(
            _render(VERIFY_CUSTOMER_PROMPT, self.session.userdata)
        )
        # The task's own instructions describe this step; let them drive the opening.
        self.session.generate_reply()

    @function_tool
    async def find_or_create_customer(
        self,
        ctx: RunContext,
        phone: Annotated[
            str,
            Field(
                description="Exact confirmed phone; 10 to 15 digits with no inferred country code"
            ),
        ],
    ) -> dict:
        """Look up or create one salon customer from the exact confirmed phone number. Use only after the digit readback got a clear yes. Reuse the record that already owns the number, or create one for a number that is new. Never guess a number or pass one the caller has not confirmed."""
        result = tools.find_or_create_customer.find_or_create_customer(phone=phone)
        if inspect.isawaitable(result):
            result = await result
        return result

    @function_tool
    async def finish(
        self,
        ctx: RunContext,
        customer: Customer,
        customer_phone: Phone,
        summary: str,
        unserved_request: Annotated[
            str,
            Field(
                description="Leave empty unless the caller asked for something this step cannot serve. Then put that request here in one short plain sentence, in the caller's own terms, so the agent that owns this step can take it."
            ),
        ] = "",
    ) -> str | None:
        """Record the result of this step and finish. complete() is the sole
        resolution; do not relay anything after it."""
        # Validated before anything is recorded, and before the terminal claim:
        # a value that does not fit its declared type never enters the state,
        # the previous contents stand, and the message goes back to the model so
        # it can correct itself on the next turn instead of the step recording
        # something wrong.
        try:
            _values = _typed_result(
                "verify_customer",
                {
                    "customer": customer,
                    "customer_phone": customer_phone,
                    "summary": summary,
                },
            )
        except _StateRefused as refused:
            logger.warning("finish %s: %s", "verify_customer", refused.message)
            return f"Not recorded: {refused.message}. Ask again, then call finish with a value that fits."
        self.complete(_task_result(_values, unserved_request))


# --- session ---------------------------------------------------------------
def prewarm(proc: JobProcess) -> None:
    # The FLOOR: how long silence has to last before the runtime treats the
    # caller as finished. Authored as endpointing_delay.
    #
    # Always explicit. Leaving it off inherited Silero's own 0.55s, which is
    # slower than the turn detector wants and was never anybody's choice. Hard
    # floor is 0.25s; the turn detector raises at session start below it.
    #
    # Lowering this alone does not shorten a turn. The ceiling is in the session's
    # turn_handling endpointing below, and that is where a 2.5s turn came from.
    proc.userdata["vad"] = silero.VAD.load(min_silence_duration=0.4)
    # Read, split and embed every knowledge base here, not on the call path.
    # LiveKit documents prewarm as the place for static data a job needs, and it
    # runs once per worker process before any job is accepted, so a caller never
    # waits for indexing and a failure stops the worker from reporting ready.
    knowledge.build_indexes()


MAX_SESSIONS = 10


def telephony_load(agent_server: AgentServer) -> float:
    return min(len(agent_server.active_jobs) / MAX_SESSIONS, 1.0)


server = AgentServer(
    port=int(os.getenv("UNMUTE_AGENT_HEALTH_PORT", "8081")),
    drain_timeout=900,
    load_threshold=1.0,
)
server.load_fnc = telephony_load
server.setup_fnc = prewarm


@server.rtc_session(agent_name="salon-concierge-v2-livekit")
async def entrypoint(ctx: JobContext) -> None:
    require_env()
    session = AgentSession[Userdata](
        userdata=Userdata(),
        stt=slng.STT(
            api_key=os.environ["SLNG_API_KEY"],
            model="soniox/speech-ai:rt-v5",
            language="en",
            world_part_override="eu",
        ),
        llm=openai.LLM(
            api_key=os.environ["OPENAI_API_KEY"],
            model="gpt-5.6-luna",
            reasoning_effort="none",
        ),
        tts=slng.TTS(
            api_key=os.environ["SLNG_API_KEY"],
            voice="62ae83ad-4f6a-430b-af41-a9bede9286ca",
            model="cartesia/sonic:3.5",
            language="en",
            warm_standby_enabled=True,
            world_part_override="eu",
        ),
        turn_handling=TurnHandlingOptions(
            turn_detection=inference.TurnDetector(version="v1-mini"),
            # The CEILING, and the shortest wait the runtime will consider.
            # pace: balanced. Without this the streaming defaults apply and
            # max_delay is 2.5s, which no package could reach.
            endpointing={"mode": "dynamic", "min_delay": 0.3, "max_delay": 1.6},
            interruption={"enabled": True},
            preemptive_generation={"enabled": True},
        ),
        vad=ctx.proc.userdata["vad"],
        user_away_timeout=15,
    )

    setup_langfuse(
        ctx,
        session,
        metadata={
            "langfuse.session.id": ctx.room.name,
            "langfuse.trace.name": "concierge" + "-" + "salon-concierge-v2-livekit",
        },
    )

    # Inert unless the dev loop set UNMUTE_DEV_METRICS. Reads session events, so
    # it never touches the tracer provider an opt-in trace export would own.
    install_dev_metrics(session)

    @session.on("metrics_collected")
    def _on_metrics_collected(ev: MetricsCollectedEvent) -> None:
        metrics.log_metrics(ev.metrics)

    async def _end_if_still_away() -> None:
        await asyncio.sleep(30)  # inactivity.end_after
        if session.user_state == "away":
            session.shutdown()

    @session.on("user_state_changed")
    def _on_user_state_changed(ev) -> None:
        if ev.new_state != "away":
            return
        session.generate_reply(
            instructions="The caller went quiet. Briefly check whether they are still there."
        )
        asyncio.create_task(_end_if_still_away())

    metadata = _livekit_job_metadata(ctx.job.metadata)
    call_start = _dispatched_call_start(metadata)
    outbound_job = "phone_number" in metadata
    if outbound_job:
        require_call_env()
    await ctx.connect()

    if outbound_job:
        raise RuntimeError("this LiveKit SIP route does not accept outbound jobs")
    else:
        participant = await ctx.wait_for_participant()
    if (
        not outbound_job
        and participant.kind == rtc.ParticipantKind.PARTICIPANT_KIND_SIP
    ):
        require_call_env()
    # Bound whether or not a SIP participant arrived, so the pre-fetch below can
    # read it either way: a web caller has no carrier facts and every entry that
    # reads one skips, which is the documented behaviour and not an error.
    call_context = None
    if (
        not outbound_job
        and participant.kind == rtc.ParticipantKind.PARTICIPANT_KIND_SIP
    ):
        call_context = _livekit_call_context(ctx.room.name, participant, metadata)
        _hydrate_livekit_context(session.userdata, call_context)
    if not outbound_job:
        _hydrate_call_start(session.userdata, call_start)
    if not outbound_job:
        await _prefetch(session.userdata, call_context)
    if not outbound_job:
        await session.start(agent=Concierge(initial=True), room=ctx.room)
    await _livekit_entry_greeting(session)

    async def _max_duration() -> None:
        await asyncio.sleep(900)
        session.shutdown()  # conversation.max_duration

    asyncio.create_task(_max_duration())


# No __main__ block: this module is started through livekit-agents' supported
# CLI, `python -m livekit.agents start agent.py`, which imports it and finds the
# `server` above. The older per-script entry point goes through a CLI upstream
# has deprecated and will remove.
