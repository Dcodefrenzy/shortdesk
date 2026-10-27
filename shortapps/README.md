# ShortApps

The apps every phone starts with. Each folder is one app, installed onto the Phone Desk
when Shortdesk starts (and updated with Shortdesk, unless its owner changed it).

```
tasks/
  app.json      what the app is and what it keeps
  web/          its page: index.html and anything it loads
```

`app.json`:

```json
{
  "id": "tasks",
  "name": "Tasks",
  "icon": "✅",
  "color": "#ffffff",
  "description": "One sentence on what it's for. Generic: never about one person.",
  "version": "1.0.0",
  "collections": ["tasks"],
  "copiedFrom": "",
  "logins": []
}
```

- `collections` are the kinds of records the app keeps. Records are JSON objects;
  each has an id, a version that goes up on every change, and a history.
- `logins` lists websites the app's work needs a login for (shown in the app, saved
  in the Vault).
- `copiedFrom` is set when an app is a changed copy of another one.

The page loads `shortapps.js` (served next to every app) and reaches its data through
it; it runs walled off, with no network and no cookies. See `sdk.js` for the calls.

Agents build apps on their own desk and install them with the `apps` tool. Data never
leaves the Phone Desk with an app: publishing an app shares app.json and web/ only.
