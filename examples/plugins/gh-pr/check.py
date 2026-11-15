#!/usr/bin/env python3
"""regia plugin: the agent's GitHub PR in detail, via gh.

Fields: state and size, CI progress (which checks failed), reviews, comments.
Badge: CI at a glance. Attention: failing CI or changes requested.
Actions: copy the failing logs (to paste into the agent), re-run failed jobs,
mark a draft ready for review.

Protocol: docs/plugins.md in the regia repo.
"""
import json
import re
import subprocess
import sys
from datetime import datetime, timezone

FIELDS = ("number,url,state,isDraft,title,reviewDecision,latestReviews,reviewRequests,"
          "comments,reviews,statusCheckRollup,additions,deletions,mergeable")


def gh(*args, cwd=None):
    p = subprocess.run(["gh", *args], capture_output=True, text=True, cwd=cwd, timeout=15)
    if p.returncode != 0:
        raise RuntimeError(p.stderr.strip().splitlines()[-1] if p.stderr.strip() else f"gh exit {p.returncode}")
    return p.stdout


def pr_ref(inp):
    """The PR to look at: what regia knows, else a recorded link, else the branch."""
    if inp.get("pr") and inp["pr"].get("url"):
        return inp["pr"]["url"]
    for link in reversed(inp["agent"].get("links") or []):
        if link.get("kind") == "pr" and link.get("url"):
            return link["url"]
    b = inp["agent"].get("branch") or ""
    return b if b and b not in ("main", "master", "develop", "trunk") else None


def ago(ts):
    if not ts:
        return ""
    t = datetime.fromisoformat(ts.replace("Z", "+00:00"))
    s = int((datetime.now(timezone.utc) - t).total_seconds())
    for unit, n in (("d", 86400), ("h", 3600), ("m", 60)):
        if s >= n:
            return f"{s // n}{unit} ago"
    return "just now"


def checks(rollup):
    """(passed, running, failed names, failed run ids, failed job ids, total) from statusCheckRollup."""
    passed, running, failed, run_ids, job_ids = 0, 0, [], set(), []
    for c in rollup or []:
        name = c.get("name") or c.get("context") or "?"
        concl = (c.get("conclusion") or c.get("state") or "").upper()
        status = (c.get("status") or "").upper()
        if concl in ("FAILURE", "ERROR", "TIMED_OUT", "CANCELLED", "ACTION_REQUIRED", "STARTUP_FAILURE"):
            failed.append(name)
            m = re.search(r"/actions/runs/(\d+)", c.get("detailsUrl") or "")
            if m:
                run_ids.add(m.group(1))
            j = re.search(r"/job/(\d+)", c.get("detailsUrl") or "")
            if j:
                job_ids.append((name, j.group(1)))
        elif (status and status != "COMPLETED") or concl in ("", "PENDING", "EXPECTED"):
            running += 1
        else:
            passed += 1
    return passed, running, failed, sorted(run_ids), job_ids, len(rollup or [])


def bar(done, total, width=10):
    if total == 0:
        return ""
    k = round(width * done / total)
    return "▰" * k + "▱" * (width - k)


def view(ref, cwd):
    return json.loads(gh("pr", "view", ref, "--json", FIELDS, cwd=cwd))


def refresh(inp, cwd):
    ref = pr_ref(inp)
    if not ref:
        return {}
    try:
        pr = view(ref, cwd)
    except RuntimeError as e:
        if "no pull requests found" in str(e).lower():
            return {}  # branch without a PR: nothing to say
        raise

    state = "draft" if pr["isDraft"] and pr["state"] == "OPEN" else pr["state"].lower()
    fields, actions = [], []

    # ── state and size
    st = {"merged": "ok", "closed": "dim", "draft": "dim"}.get(state, "")
    head = f"#{pr['number']} {state} · +{pr['additions']} −{pr['deletions']}"
    if state in ("open", "draft") and pr.get("mergeable") == "CONFLICTING":
        head += " · conflicts"
        st = "error"
    fields.append({"label": "pr", "text": head, "status": st, "url": pr["url"]})

    # ── CI progress
    passed, running, failed, run_ids, _, total = checks(pr["statusCheckRollup"])
    badge = None
    if total:
        done = passed + len(failed)
        parts = [f"{bar(done, total)} {done}/{total}"]
        if running:
            parts.append(f"{running} running")
        if failed:
            parts.append(f"{len(failed)} failed: {', '.join(failed[:3])}")
        cst = "error" if failed else ("warn" if running else "ok")
        fields.append({"label": "ci", "text": " · ".join(parts), "status": cst})
        badge = {"text": "✗" if failed else ("…" if running else "✓"), "status": cst}

    # ── reviews
    by = {}
    for r in pr.get("latestReviews") or []:
        by[r["author"]["login"]] = r["state"]
    approved = [u for u, s in by.items() if s == "APPROVED"]
    changes = [u for u, s in by.items() if s == "CHANGES_REQUESTED"]
    commented = [u for u, s in by.items() if s == "COMMENTED"]
    waiting = [r.get("login") or r.get("name") or r.get("slug") for r in pr.get("reviewRequests") or []]
    rv = []
    if approved:
        rv.append("approved by " + ", ".join(approved))
    if changes:
        rv.append("changes requested by " + ", ".join(changes))
    if commented:
        rv.append("commented: " + ", ".join(commented))
    if waiting:
        rv.append("waiting on " + ", ".join(waiting))
    fields.append({"label": "reviews", "text": " · ".join(rv) or "no reviews yet",
                   "status": "error" if changes else ("ok" if approved else "dim")})

    # ── conversation
    comments = pr.get("comments") or []
    n_review_bodies = sum(1 for r in pr.get("reviews") or [] if (r.get("body") or "").strip())
    talk = f"{len(comments)} comment{'s' * (len(comments) != 1)}"
    if n_review_bodies:
        talk += f" · {n_review_bodies} review note{'s' * (n_review_bodies != 1)}"
    if comments:
        last = comments[-1]
        talk += f" · last by {last['author']['login']} {ago(last.get('createdAt'))}"
    fields.append({"label": "talk", "text": talk, "status": "" if comments else "dim"})

    # ── actions, only those that make sense now
    if failed:
        actions.append({"id": "copy-failed", "label": "copy failing CI logs (to paste into the agent)"})
        if run_ids and not running:  # GitHub refuses to re-run a run that's still going
            actions.append({"id": "rerun-failed", "label": f"re-run failed jobs ({len(failed)})", "confirm": True})
    if state == "draft":
        actions.append({"id": "ready", "label": "mark ready for review", "confirm": True})

    out = {"fields": fields, "actions": actions}
    if badge:
        out["badge"] = badge
    why = []
    if failed and state in ("open", "draft"):
        why.append(f"CI failing ({', '.join(failed[:2])})")
    if changes:
        why.append("changes requested by " + ", ".join(changes))
    if why:
        out["attention"] = " · ".join(why)
    # poll faster while CI runs; a finished PR never changes
    if state in ("merged", "closed"):
        out["every"] = "never"
    elif running:
        out["every"] = "20s"
    return out


def action(inp, cwd, name):
    ref = pr_ref(inp)
    if not ref:
        raise RuntimeError("no PR for this agent")
    pr = view(ref, cwd)
    _, _, failed, run_ids, job_ids, _ = checks(pr["statusCheckRollup"])
    if name == "copy-failed":
        # per job: works while other jobs of the same run are still going
        chunks = []
        for job, jid in job_ids:
            log = gh("api", "--allow-escape-sequences", f"repos/{repo_of(pr['url'])}/actions/jobs/{jid}/logs")
            log = re.sub(r"\x1b\[[0-9;]*[A-Za-z]", "", log)  # colors would be noise in a chat
            log = re.sub(r"^\S+Z ", "", log, flags=re.M)      # GitHub's per-line timestamps
            tail = "\n".join(log.strip().splitlines()[-40:])
            chunks.append(f"--- {job} (job {jid}) ---\n{tail}")
        text = f"CI is failing on {pr['url']} ({', '.join(failed)}). Logs:\n\n" + "\n\n".join(chunks)
        return {"message": f"{len(failed)} failing check(s) copied", "copy": text}
    if name == "rerun-failed":
        for rid in run_ids:
            gh("run", "rerun", rid, "--failed", "--repo", repo_of(pr["url"]))
        return {"message": f"re-running failed jobs in {len(run_ids)} run(s)"}
    if name == "ready":
        gh("pr", "ready", pr["url"])
        return {"message": f"#{pr['number']} is ready for review"}
    raise RuntimeError(f"unknown action {name}")


def repo_of(url):
    m = re.match(r"https://[^/]+/([^/]+/[^/]+)/pull/", url)
    return m.group(1) if m else ""


def main():
    inp = json.load(sys.stdin)
    cwd = None  # regia already runs us in the agent's directory (or ours, if it's gone)
    try:
        if inp["event"] == "action":
            out = action(inp, cwd, inp.get("action"))
        else:
            out = refresh(inp, cwd)
    except (RuntimeError, subprocess.TimeoutExpired) as e:
        print(f"gh-pr: {e}", file=sys.stderr)  # regia shows this line dim on the card
        sys.exit(1)
    json.dump(out, sys.stdout)


if __name__ == "__main__":
    main()
