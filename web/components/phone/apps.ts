// The apps on the phone. System apps are part of Shortdesk; the rest arrive with the
// app runtime (docs/SHORTAPPS.md) and show as "coming" until then.
export type AppId =
  | "messages"
  | "tasks"
  | "reminders"
  | "calendar"
  | "journal"
  | "notes"
  | "contacts"
  | "documents"
  | "files"
  | "vault"
  | "settings"
  | "store";

export type PhoneApp = {
  id: AppId;
  name: string;
  icon: string;
  tile: string; // background of the icon tile
  ready: boolean;
  about: string;
};

export const phoneApps: PhoneApp[] = [
  { id: "messages", name: "Messages", icon: "💬", tile: "bg-[#b9e4ff]", ready: false,
    about: "Everything your agent needs to tell or ask you, sorted by what it's about. Telegram mirrors it." },
  { id: "tasks", name: "Tasks", icon: "✅", tile: "bg-white", ready: false,
    about: "What you've asked for and where each job stands: to do, running on a desk, waiting for you, done." },
  { id: "reminders", name: "Reminders", icon: "⏰", tile: "bg-[#ffd0d0]", ready: false,
    about: "Every reminder in one list. When one fires, your agent asks what to do about it." },
  { id: "calendar", name: "Calendar", icon: "📅", tile: "bg-[#ffd66b]", ready: false,
    about: "Your Google or Outlook calendar, with your agent's plans shown on top." },
  { id: "journal", name: "Journal", icon: "📒", tile: "bg-[#8fd3ff]", ready: false,
    about: "Your agent's daily notes about you and the work, so it remembers yesterday." },
  { id: "notes", name: "Notes", icon: "🗒️", tile: "bg-[#ffe8b5]", ready: false,
    about: "A scratch pad you and your agent share." },
  { id: "contacts", name: "Contacts", icon: "👥", tile: "bg-[#cfe9d8]", ready: false,
    about: "People, who they are to you, and everything that happened with them." },
  { id: "documents", name: "Documents", icon: "📁", tile: "bg-[#d9f5ee]", ready: false,
    about: "Your papers in one place: contracts, payslips, receipts, with expiry dates." },
  { id: "files", name: "Files", icon: "📂", tile: "bg-[#e3d1ff]", ready: true,
    about: "What your agent saved: tables, documents, images, web pages, and code to review." },
  { id: "vault", name: "Vault", icon: "🔐", tile: "bg-[#2b3a36]", ready: true,
    about: "Your logins. Agents use them to sign in, but never see the passwords." },
  { id: "settings", name: "Settings", icon: "⚙️", tile: "bg-[#e9ecea]", ready: true,
    about: "AI models, websites, desks, and how Shortdesk looks." },
  { id: "store", name: "Store", icon: "＋", tile: "bg-[#f3f3f3]", ready: true,
    about: "Install apps, or copy one someone else made." },
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

export function appById(id: string | null): PhoneApp | undefined {
  return phoneApps.find((a) => a.id === id);
}
