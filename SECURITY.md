# Security policy

**English** · [Русский](SECURITY.ru.md)

## Supported versions

Zazzzor is fixed forward: a fix lands on `main` and in the next release,
not in old ones.

| Part | Supported |
|---|---|
| Server images (`zazzzor-api`, `zazzzor-caddy`, `zazzzor-backup`) | The latest image built from `main` (`main`, `latest`) and the latest `v*` tag |
| Web client | The one the server serves, so it follows the server image |
| Windows desktop client | The latest [release](https://github.com/whoaskedssselery/Zazzzor/releases/latest); the app updates itself |

If you run an older image, update to the latest one before you report:
the problem may already be fixed.

## Reporting a vulnerability

Report privately, through GitHub:
[**Report a vulnerability**](https://github.com/whoaskedssselery/Zazzzor/security/advisories/new)
(the Security tab of this repository, then "Report a vulnerability").
Only the maintainers see the report. Don't open a public issue, pull
request or discussion about it.

Reports in English or Russian are both fine. Please include:

- what is affected: the server, the web client, the desktop client or the
  deploy configs, and the image tag, commit or release version;
- what an attacker can do, and what they need first (an account, a role on
  a server, access to the network);
- the steps to reproduce it, or a proof of concept;
- a fix or a mitigation, if you have one in mind.

Never include real secrets, tokens or other people's data; describe them
or replace them with fake values.

## What happens next

The maintainers are a small group running the project in their spare time,
so there is no fixed deadline, but a report is acknowledged as soon as it
is read. Once the issue is confirmed, the fix is worked out in a private
advisory, released as new images or a new desktop release, and then the
advisory is published. You are credited in it unless you'd rather not be.

## Scope

In scope:

- the server and the realtime gateway (`server/`) and the API contract
  (`api/`);
- the web client (`web/`);
- the Windows desktop client and its updater (`desktop/`);
- the shipped deploy configs: the Compose and Swarm stacks, Caddy, the
  backup image and the scripts in `deploy/`, as documented in the
  [server guide](deploy/README.md).

Out of scope:

- a misconfiguration of someone's own server, such as a weak password, an
  exposed Postgres port, a leaked `.env` or a skipped update, unless the
  [server guide](deploy/README.md) told them to do it;
- vulnerabilities in third-party software (Docker, Postgres, LiveKit,
  Caddy, WebView2) that Zazzzor doesn't make worse; report those upstream;
- denial of service by flooding a server with traffic, and social
  engineering of its users.
