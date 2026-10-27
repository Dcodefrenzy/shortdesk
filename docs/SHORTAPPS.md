# ShortApps plan

The agent is a person and its desk is its phone. You chat with your agent; it does the
work on desks and keeps track of things in **ShortApps**: small apps on a phone you see
next to the chat.

## Words

| Word | Meaning |
|---|---|
| **Main desk** | The always-on desk you chat with. It answers, files things, does quick jobs, and runs the apps. |
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
| Runtime | Go, part of Shortdesk: serves every app, gives each one its data API from `app.json` |
| Custom logic | Optional small TypeScript functions |
| Database | SQLite, one file per app, behind a data layer that can switch to Postgres |
| Agent access | The same data API, through an `apps` tool / MCP |

```
calories/
  app.json      what it keeps (collections and fields), logins and connections it needs
  web/          the screens
  functions/    optional custom logic
  data/app.db   its data: never published
```

Publishing shares `app.json`, the code and screenshots, tagged with what it was copied
from. Never the data, never passwords (those live in the Vault).

## Build order

1. **Dashboard shell** (now): Chat | Desktop tabs, the phone on the right, and the
   system apps on today's backend: **Vault** (real Vaultwarden logins, add-login form),
   **Settings** (AI models, router, websites, shared folders, desk software, theme),
   **Files** (what the agent saved), **Store** (what's coming). Onboarding as a
   checklist on the phone's home screen. The chat talks to the main desk.
2. **Main desk + helpers**: an always-on main desk, Tasks as the queue, helper desks
   started and put to sleep automatically, scratchbooks.
3. **App runtime**: `app.json` → data API, SQLite per app behind a switchable data
   layer, apps served on the phone, the `apps` tool for agents.
4. **First apps**: Messages, Tasks, Reminders (with the Scheduler and Telegram
   notifications), Journal, Notes.
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
