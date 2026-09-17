# Templates

Templates moved out of this repository. They live here:

## [**github.com/slng-ai/unmute-templates**](https://github.com/slng-ai/unmute-templates)

Each folder there is one finished voice agent. You can run it, copy it, and
change it. The repository README lists every template, what it shows, and which
API keys it needs.

## Run one

```sh
git clone https://github.com/slng-ai/unmute-templates
cd unmute-templates

unmute validate single-prompt              # check the files
unmute compile single-prompt               # write the projects
unmute dev single-prompt --target pipecat  # talk to it in your browser
```

Then copy the folder you like and change the `name:` line in its `agent.yaml`.
That is your agent.

## Add one

Open a pull request on
[slng-ai/unmute-templates](https://github.com/slng-ai/unmute-templates), not
here. A good template is one folder showing one idea. It validates and
compiles, it carries a README saying what to listen for and which keys it
needs, it adds a row to that repository's table, and it holds no secrets.

[CONTRIBUTING.md](../CONTRIBUTING.md) is the long version, and it also covers
changes to Unmute itself, which are a different job with a longer checklist.

## Why this folder is still here

So an old link to `templates/` lands on these words instead of a 404. Nothing
else lives here.
