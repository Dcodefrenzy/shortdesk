"use client";

import { useEffect, useState } from "react";
import { api, type Project } from "@/lib/api";
import { Button, ErrorNote, Spinner } from "./ui";

// Projects with this desk's unmerged work, and the review/merge view for one of them.
export function ProjectList({ projects, onOpen }: { projects: Project[]; onOpen: (name: string) => void }) {
  if (projects.length === 0) return null;
  return (
    <div className="border-b border-border">
      <p className="px-3 pt-3 pb-1 text-xs font-medium text-muted">Projects</p>
      <ul>
        {projects.map((p) => (
          <li key={p.name}>
            <button
              onClick={() => onOpen(p.name)}
              className="flex w-full items-center gap-3 px-3 py-2 text-left text-sm hover:bg-background"
            >
              <span aria-hidden>⌥</span>
              <span className="min-w-0 flex-1">
                <span className="block truncate font-medium">{p.name}</span>
                <span className="block truncate text-xs text-muted">{projectState(p)}</span>
              </span>
              {pending(p) && <span className="size-2 rounded-full bg-warn" title="Changes to merge" />}
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}

export function pending(p: Project) {
  return p.ahead > 0 || p.uncommitted > 0;
}

export function projectState(p: Project) {
  if (!p.branch) return "This desk hasn't worked on it";
  const parts = [];
  if (p.ahead > 0) parts.push(`${p.ahead} ${p.ahead === 1 ? "commit" : "commits"} ready to merge`);
  if (p.uncommitted > 0) parts.push(`${p.uncommitted} uncommitted ${p.uncommitted === 1 ? "file" : "files"}`);
  return parts.length ? parts.join(" · ") : "Up to date with " + p.main;
}

export function ProjectReview({
  deskId,
  project,
  onMerged,
}: {
  deskId: string;
  project: Project;
  onMerged: () => void;
}) {
  const [diff, setDiff] = useState<string | null>(null);
  const [error, setError] = useState("");
  const [merging, setMerging] = useState(false);
  const [merged, setMerged] = useState(false);

  useEffect(() => {
    let live = true;
    api.projectDiff(deskId, project.name).then(
      (r) => live && setDiff(r.diff),
      (e: Error) => live && setError(e.message),
    );
    return () => {
      live = false;
    };
  }, [deskId, project.name, project.ahead]);

  async function merge() {
    setMerging(true);
    setError("");
    try {
      await api.mergeProject(deskId, project.name);
      setMerged(true);
      onMerged();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setMerging(false);
    }
  }

  return (
    <div className="space-y-3 p-3 text-sm">
      <div>
        <p className="font-medium">{project.name}</p>
        <p className="text-xs text-muted">
          Branch <span className="font-mono">{project.branch || "—"}</span> → <span className="font-mono">{project.main}</span>{" "}
          · {projectState(project)}
        </p>
        {project.lastCommit && <p className="mt-1 text-xs text-muted">Last commit: {project.lastCommit}</p>}
      </div>
      {project.uncommitted > 0 && (
        <p className="rounded-md bg-warn/10 px-3 py-2 text-xs text-warn">
          The agent has changes it hasn&apos;t committed yet; only committed work is merged.
        </p>
      )}
      <div className="flex flex-wrap items-center gap-2">
        <Button variant="primary" onClick={merge} disabled={merging || merged || project.ahead === 0}>
          {merging ? <Spinner /> : merged ? "Merged" : `Merge into ${project.main}`}
        </Button>
        <span className="text-xs text-muted">Local only: nothing is pushed to GitHub or Bitbucket.</span>
      </div>
      {error && <ErrorNote message={error} />}
      {diff === null && !error ? (
        <p className="flex items-center gap-2 text-muted">
          <Spinner /> Loading changes…
        </p>
      ) : diff ? (
        <pre className="overflow-x-auto rounded-md border border-border bg-background p-2 font-mono text-xs leading-5">
          {diff.split("\n").map((line, i) => (
            <span
              key={i}
              className={
                line.startsWith("+") && !line.startsWith("+++")
                  ? "block text-ok"
                  : line.startsWith("-") && !line.startsWith("---")
                    ? "block text-danger"
                    : line.startsWith("@@")
                      ? "block text-accent"
                      : "block"
              }
            >
              {line || " "}
            </span>
          ))}
        </pre>
      ) : (
        <p className="text-xs text-muted">No committed changes to merge.</p>
      )}
    </div>
  );
}
