// What the phone shows. ShortApps come from the Phone Desk (installed apps); system
// apps are part of Shortdesk itself (they hold keys, passwords and limits, which must
// stay outside every desk); "soon" apps are on their way.
export type AppKind = "short" | "system" | "soon";

export type PhoneApp = {
  id: string;
  name: string;
  icon: string;
  tile: string; // Tailwind background class, or "" when color is set
  color?: string; // a ShortApp's own icon colour
  kind: AppKind;
  about: string;
};

export const systemApps: PhoneApp[] = [
  { id: "files", name: "Files", icon: "📂", tile: "bg-[#e3d1ff]", kind: "system",
    about: "What your agent saved: tables, documents, images, web pages, and code to review." },
  { id: "vault", name: "Vault", icon: "🔐", tile: "bg-[#2b3a36]", kind: "system",
    about: "Your logins. Agents use them to sign in, but never see the passwords." },
  { id: "settings", name: "Settings", icon: "⚙️", tile: "bg-[#e9ecea]", kind: "system",
    about: "AI models, websites, desks, and how Shortdesk looks." },
  { id: "store", name: "Store", icon: "＋", tile: "bg-[#f3f3f3]", kind: "system",
    about: "Install apps, or copy one someone else made." },
];

export const soonApps: PhoneApp[] = [
  { id: "usage", name: "Token usage", icon: "📊", tile: "bg-[#ffcf9e]", kind: "soon",
    about: "What your AI costs: by desk, task and model, with limits you set so spending never runs away." },
  { id: "reminders", name: "Reminders", icon: "⏰", tile: "bg-[#ffd0d0]", kind: "soon",
    about: "Every reminder in one list. When one fires, your agent asks what to do about it." },
  { id: "calendar", name: "Calendar", icon: "📅", tile: "bg-[#ffd66b]", kind: "soon",
    about: "Your Google or Outlook calendar, with your agent's plans shown on top." },
  { id: "journal", name: "Journal", icon: "📒", tile: "bg-[#8fd3ff]", kind: "soon",
    about: "Your agent's daily notes about you and the work, so it remembers yesterday." },
  { id: "notes", name: "Notes", icon: "🗒️", tile: "bg-[#ffe8b5]", kind: "soon",
    about: "A scratch pad you and your agent share." },
  { id: "contacts", name: "Contacts", icon: "👥", tile: "bg-[#cfe9d8]", kind: "soon",
    about: "People, who they are to you, and everything that happened with them." },
  { id: "documents", name: "Documents", icon: "📁", tile: "bg-[#d9f5ee]", kind: "soon",
    about: "Your papers in one place: contracts, payslips, receipts, with expiry dates." },
];

// Apps that will be in the Store rather than on every phone.
export const storeApps: { name: string; icon: string; about: string }[] = [
  { name: "Price watch", icon: "🏷️", about: "Products you want, their price over time, and a message when one drops under your target." },
  { name: "Tracker", icon: "🏠", about: "Anything moving through steps, like flat or job applications, with how long each has waited." },
  { name: "Automations", icon: "⚡", about: "Flows: when something happens, your agent does something and tells you." },
  { name: "Deliveries", icon: "📦", about: "Every parcel you're waiting for, read from your order emails." },
  { name: "Stocks", icon: "📈", about: "Stocks you watch, with your own gains and losses and why prices moved." },
  { name: "Accounts", icon: "💶", about: "Money in and out, subscriptions, and where you could save." },
];
