# ShortApps plan

The agent is a person and its desk is its phone. You chat with your agent; it does the
work on desks and keeps track of things in **ShortApps**: small apps on a phone you see
next to the chat.

## Words

| Word | Meaning |
|---|---|
| **Main desk** | The virtual desktop that never dies. Its agent answers you fast, orchestrates helper desks, and it runs the Phone Desk. |
| **Phone Desk** | The software on the main desk that serves the mini apps (`shortdesk phone-host`). Apps and their data live on their own volume (`shortdesk-phone`), so they outlive any desk. |
| **Helper desk** | Extra desks the main desk starts for long jobs, and puts to sleep after. |
| **ShortApp** | A small app on the phone that holds and shows information (Tasks, Messages, Berlin flats…). Apps store and show; agents do the work. |
| **System app** | An app that is part of Shortdesk itself: Vault, Settings, Files, Store. |
| **Scratchbook** | The group chat behind a task, where the main desk and helpers post short updates. The full stream is kept as a log. |
| **Service** | Shared machinery apps use: Scheduler, Notifications, Reminders delivery. |
| **Connection** | A way to bring data in or reach people: Gmail, Calendar, bank, Telegram. Keys stay in the Vault. |

Only two can change an app's data: you, on the web, and the agents working in desks.
Telegram is a way to talk to your main desk, never to apps directly.

## The screen

```
┌──────────────────────────────────────────┬──────────────────┐
│  Your agent          [ Chat | Desktop ]  │   ┌──────────┐   │
│                                          │   │  phone   │   │
│  Chat: talk to the main desk             │   │  home +  │   │
│  Desktop: watch any desk, take over      │   │  apps    │   │
│                                          │   └──────────┘   │
└──────────────────────────────────────────┴──────────────────┘
```

## How a ShortApp is built

| Part | Choice |
|---|---|
| Screens | TypeScript + React, built to static files |
| Runtime | The Phone Desk (Go, `internal/phone`): installs apps, serves their pages walled off, gives each one its data API |
| Custom logic | Optional small TypeScript functions |
| Database | SQLite, one file per app, behind a data layer that can switch to Postgres |
| Agent access | The same data API through the `apps` tool on every desk, with a key Shortdesk signs per task; MCP later |

```
calories/
  app.json      id, name, icon, colour, description, version, collections, logins it needs
  web/          the screens: index.html loads shortapps.js for its data
  (data)        kept by the Phone Desk, one SQLite file per app: never published
```

Publishing shares `app.json`, the code and screenshots, tagged with what it was copied
from. Never the data, never passwords (those live in the Vault).

## Build order

1. **Dashboard shell** (done): Chat | Desktop tabs, the phone on the right, and the
   system apps on today's backend: **Vault** (real Vaultwarden logins, add-login form),
   **Settings** (AI models, router, websites, shared folders, desk software, theme),
   **Files** (what the agent saved), **Store** (what's coming). Onboarding as a
   checklist on the phone's home screen. The chat talks to the main desk.
2. **Phone Desk** (done): runs on the main desk, SQLite per app behind a switchable
   data layer (`internal/appdata`), apps served walled off on the phone, the `apps`
   tool for agents (use, message, task, build, install, undo). First apps: Messages,
   Tasks (`shortapps/`).
3. **Main desk + helpers** (done): the main desk never expires and orchestrates; it
   starts helper desks (up to 3) with lifetimes for long jobs, reads their Scratchbook
   updates, passes their questions to the main chat, and takes them back after a
   restart; the chat is always open.
4. **More apps**: Token usage (spend and limits, a system app), Reminders (with the
   Scheduler and Telegram notifications), Journal, Notes.
5. **Store apps**: Price watch, Tracker (Berlin flats), Calendar, Contacts, Documents,
   Automations, Deliveries.
6. **Publishing**: install, copy with a "copied from" tag, publish.

## Onboarding

1. `shortdesk up` opens the dashboard, logged in.
2. Connect an AI (Claude login, a Claude token, or your own model keys).
3. Start your main desk (about a minute). The phone shows the apps.
4. Add the logins your apps need, from inside each app or the Vault.
5. Optional: Telegram, Google Calendar, Gmail.
6. Give your agent its first job.
