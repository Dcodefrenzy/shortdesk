"use client";

import { useEffect, useMemo, useState } from "react";
import { api, type Output, type Project } from "@/lib/api";
import { compareCells, parseCsv } from "@/lib/csv";
import { age } from "@/lib/format";
import { Markdown } from "./markdown";
import { ProjectList, ProjectReview } from "./project-review";
import { Button, Spinner } from "./ui";

type Kind = "doc" | "table" | "image" | "pdf" | "web" | "text" | "file";

export function kindOf(o: Output): Kind {
  const p = o.path.toLowerCase();
  if (/\.(md|markdown)$/.test(p)) return "doc";
  if (p.endsWith(".csv")) return "table";
  if (o.type.startsWith("image/")) return "image";
  if (p.endsWith(".pdf")) return "pdf";
  if (/\.html?$/.test(p)) return "web";
  if (o.type.startsWith("text/") || /\.(json|txt|log|py|js|ts|go|sh|yaml|yml)$/.test(p)) return "text";
  return "file";
}

const icons: Record<Kind, string> = { doc: "📄", table: "▦", image: "🖼", pdf: "📕", web: "🌐", text: "⌨", file: "📦" };

export function OutputIcon({ output }: { output: Output }) {
  return <span aria-hidden>{icons[kindOf(output)]}</span>;
}

function size(n: number) {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

// Side panel listing the agent's output files, with a viewer for the selected one.
// Selections starting with this prefix are projects; others are output file paths.
export const PROJECT = "project:";

export function ArtifactsPanel({
  deskId,
  outputs,
  projects,
  selected,
  onSelect,
  onClose,
  onProjectsChanged,
}: {
  deskId: string;
  outputs: Output[];
  projects: Project[];
  selected: string | null;
  onSelect: (path: string | null) => void;
  onClose: () => void;
  onProjectsChanged: () => void;
}) {
  const current = outputs.find((o) => o.path === selected);
  const project = selected?.startsWith(PROJECT) ? projects.find((p) => PROJECT + p.name === selected) : undefined;
  return (
    <aside className="flex h-full w-full flex-col border-l border-border bg-surface">
      <div className="flex items-center gap-2 border-b border-border px-3 py-2">
        {project ? (
          <>
            <Button variant="ghost" onClick={() => onSelect(null)} aria-label="All artifacts">
              ←
            </Button>
            <span className="min-w-0 flex-1 truncate text-sm font-medium">⌥ {project.name}</span>
          </>
        ) : current ? (
          <>
            <Button variant="ghost" onClick={() => onSelect(null)} aria-label="All artifacts">
              ←
            </Button>
            <span className="min-w-0 flex-1 truncate text-sm font-medium" title={current.path}>
              <OutputIcon output={current} /> {current.path}
            </span>
            <a
              href={api.outputUrl(deskId, current.path, "download")}
              className="rounded-md px-2 py-1 text-sm text-muted hover:bg-background hover:text-foreground"
            >
              Download
            </a>
          </>
        ) : (
          <span className="flex-1 text-sm font-medium">Artifacts</span>
        )}
        <Button variant="ghost" onClick={onClose} aria-label="Close artifacts">
          ✕
        </Button>
      </div>
      <div className="min-h-0 flex-1 overflow-auto">
        {project ? (
          <ProjectReview deskId={deskId} project={project} onMerged={onProjectsChanged} />
        ) : current ? (
          <Viewer key={current.path + current.modified} deskId={deskId} output={current} />
        ) : outputs.length === 0 && projects.length === 0 ? (
          <p className="p-4 text-sm text-muted">
            Nothing yet. Files the agent saves (tables, documents, images, PDFs, web pages) appear here.
          </p>
        ) : (
          <>
          <ProjectList projects={projects} onOpen={(name) => onSelect(PROJECT + name)} />
          {outputs.length > 0 && <p className="px-3 pt-3 pb-1 text-xs font-medium text-muted">Files</p>}
          <ul className="divide-y divide-border">
            {outputs.map((o) => (
              <li key={o.path}>
                <button
                  onClick={() => onSelect(o.path)}
                  className="flex w-full items-center gap-3 px-3 py-2.5 text-left text-sm hover:bg-background"
                >
                  <OutputIcon output={o} />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate font-medium">{o.path}</span>
                    <span className="text-xs text-muted">
                      {size(o.size)} · {age(o.modified)} ago
                    </span>
                  </span>
                </button>
              </li>
            ))}
          </ul>
          </>
        )}
      </div>
    </aside>
  );
}

function useText(url: string) {
  const [state, setState] = useState<{ text?: string; error?: string }>({});
  useEffect(() => {
    let live = true;
    fetch(url, { credentials: "same-origin" })
      .then((r) => (r.ok ? r.text() : Promise.reject(new Error(`HTTP ${r.status}`))))
      .then(
        (text) => live && setState({ text }),
        (e: Error) => live && setState({ error: e.message }),
      );
    return () => {
      live = false;
    };
  }, [url]);
  return state;
}

function Viewer({ deskId, output }: { deskId: string; output: Output }) {
  const url = api.outputUrl(deskId, output.path);
  switch (kindOf(output)) {
    case "image":
      return (
        // eslint-disable-next-line @next/next/no-img-element -- desk files, not optimizable assets
        <img src={api.outputUrl(deskId, output.path, "preview")} alt={output.path} className="mx-auto max-w-full p-3" />
      );
    case "pdf":
      return <iframe src={url} title={output.path} className="h-full w-full border-0" />;
    case "web":
      // No scripts and a unique origin: the page can't touch the dashboard.
      return (
        <iframe
          src={api.outputUrl(deskId, output.path, "preview")}
          title={output.path}
          sandbox=""
          className="h-full w-full border-0 bg-white"
        />
      );
    case "doc":
      return <TextViewer url={url} render={(t) => <div className="p-4"><Markdown text={t} /></div>} />;
    case "table":
      return <TextViewer url={url} render={(t) => <Table text={t} />} />;
    case "text":
      return <TextViewer url={url} render={(t) => <pre className="p-4 font-mono text-xs whitespace-pre-wrap">{t}</pre>} />;
    default:
      return (
        <div className="p-4 text-sm text-muted">
          No preview for this kind of file.{" "}
          <a href={api.outputUrl(deskId, output.path, "download")} className="underline">
            Download it
          </a>
          .
        </div>
      );
  }
}

function TextViewer({ url, render }: { url: string; render: (text: string) => React.ReactNode }) {
  const { text, error } = useText(url);
  if (error) return <p className="p-4 text-sm text-danger">Couldn&apos;t load the file ({error}).</p>;
  if (text === undefined)
    return (
      <p className="flex items-center gap-2 p-4 text-sm text-muted">
        <Spinner /> Loading…
      </p>
    );
  return <>{render(text)}</>;
}

function Table({ text }: { text: string }) {
  const rows = useMemo(() => parseCsv(text), [text]);
  const [sort, setSort] = useState<{ col: number; dir: 1 | -1 } | null>(null);
  const [header, ...body] = rows;
  const sorted = useMemo(() => {
    if (!sort) return body;
    return [...body].sort((a, b) => sort.dir * compareCells(a[sort.col] ?? "", b[sort.col] ?? ""));
  }, [body, sort]);
  if (!header) return <p className="p-4 text-sm text-muted">Empty table.</p>;
  return (
    <div className="p-3">
      <p className="mb-2 text-xs text-muted">
        {body.length} {body.length === 1 ? "row" : "rows"} · click a column to sort
      </p>
      <div className="overflow-x-auto rounded-md border border-border">
        <table className="w-full text-sm">
          <thead className="bg-background">
            <tr>
              {header.map((h, i) => (
                <th key={i} className="border-b border-border px-3 py-2 text-left font-medium whitespace-nowrap">
                  <button
                    onClick={() => setSort(sort?.col === i ? { col: i, dir: sort.dir === 1 ? -1 : 1 } : { col: i, dir: 1 })}
                    className="hover:underline"
                  >
                    {h}
                    {sort?.col === i ? (sort.dir === 1 ? " ↑" : " ↓") : ""}
                  </button>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {sorted.map((r, i) => (
              <tr key={i} className="border-b border-border last:border-0">
                {header.map((_, j) => (
                  <td key={j} className="px-3 py-1.5 align-top">
                    {r[j] ?? ""}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
