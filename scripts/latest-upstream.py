#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Compares the upstream pin of one adapter with the latest upstream.

    scripts/latest-upstream.py <adapter>

Prints key=value lines (the format of $GITHUB_OUTPUT):

    pin=<UPSTREAM_VERSION of the adapter Dockerfile>
    pin_sha=<UPSTREAM_SHA of the adapter Dockerfile>
    latest=<the latest upstream, as a version label of the same form as the pin>
    latest_sha=<its commit, where it is known>
    update=yes|no
    source=<where the latest upstream was looked up>
    note=<anything worth knowing about that lookup, or nothing>

Two kinds of pin:

- A release (libplctag, gologix, cpppo, pycomm3, pylogix, nodeeip): the latest
  is the newest release, looked up in the upstream's git tags, PyPI or npm;
  update = the version differs. Pre-releases are never "latest", except for
  gologix, whose every tag is a pre-release (vX.Y.Z-beta).
- A default-branch commit, because upstream has no (recent) release (opener,
  eipscanner, etherip, eiprs, eeip, lokieip): the latest is the head of the
  default branch; update = the pinned commit is not the head. The label is
  computed the way the pin's was (git describe, crate version, assembly
  version or Go pseudo-version), and the newest release tag, where there is
  one, is reported in the note.

Nothing is modified.
"""

import json
import os
import re
import subprocess
import sys
import tempfile
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
AGENT = "ethernetip-interop latest-upstream (https://github.com/otfabric/ethernetip-interop)"


def fetch(url):
    request = urllib.request.Request(url, headers={"User-Agent": AGENT, "Accept": "application/json, */*"})
    with urllib.request.urlopen(request, timeout=30) as response:
        return response.read().decode()


def key(version, prerelease=None):
    """Sort key of a stable version (or of a pre-release with the suffix
    prerelease, e.g. "-beta"); None for anything else."""
    suffix = re.escape(prerelease) if prerelease else ""
    match = re.fullmatch(r"[vV]?(\d+(?:\.\d+)*)" + suffix, version)
    return tuple(int(part) for part in match.group(1).split(".")) if match else None


def newest(versions, prerelease=None):
    found = [v for v in versions if key(v, prerelease) is not None]
    if not found:
        raise SystemExit("no release found upstream")
    return max(found, key=lambda v: key(v, prerelease))


def git_tags(repository):
    """Tags of repository as {tag: commit}; an annotated tag gives its commit."""
    out = subprocess.check_output(["git", "ls-remote", "--tags", repository], text=True)
    commits = {}
    for line in out.splitlines():
        sha, ref = line.split()
        tag = ref.removeprefix("refs/tags/")
        if tag.endswith("^{}"):
            commits[tag[:-3]] = sha
        else:
            commits.setdefault(tag, sha)
    return commits


def tag_commit(repository, version):
    """The commit of the release tag of version (v1.2.3 or 1.2.3), or ""."""
    tags = git_tags(repository)
    for tag in (f"v{version}", version, f"V{version}"):
        if tag in tags:
            return tags[tag]
    return ""


# ── Release pins ──────────────────────────────────────────────────────────────

def git_release(repository, name, prerelease=None):
    tags = git_tags(repository)
    latest = newest(tags, prerelease)
    return latest.lstrip("vV"), tags[latest], f"git tags of {name}", ""


def libplctag():
    return git_release("https://github.com/libplctag/libplctag.git", "libplctag/libplctag")


def gologix():
    version, sha, source, _ = git_release("https://github.com/danomagnum/gologix.git", "danomagnum/gologix", "-beta")
    return version, sha, source, "every gologix tag is a pre-release (vX.Y.Z-beta): the newest is taken as latest"


def pypi(package, repository):
    releases = json.loads(fetch(f"https://pypi.org/pypi/{package}/json"))["releases"]
    live = [v for v, files in releases.items() if files and not all(f.get("yanked") for f in files)]
    latest = newest(live)
    sha = tag_commit(repository, latest)
    return latest, sha, "PyPI", "" if sha else f"no git tag for {latest}: find its commit by hand"


def cpppo():
    return pypi("cpppo", "https://github.com/pjkundert/cpppo.git")


def pycomm3():
    return pypi("pycomm3", "https://github.com/ottowayi/pycomm3.git")


def pylogix():
    return pypi("pylogix", "https://github.com/dmroeder/pylogix.git")


def nodeeip():
    meta = json.loads(fetch("https://registry.npmjs.org/ethernet-ip"))
    live = [v for v, info in meta["versions"].items() if not info.get("deprecated")]
    latest = newest(live)
    sha = meta["versions"][latest].get("gitHead") or tag_commit("https://github.com/cmseaton42/node-ethernet-ip.git", latest)
    return latest, sha, "npm (ethernet-ip)", "" if sha else f"no gitHead or git tag for {latest}: find its commit by hand"


# ── Default-branch pins ───────────────────────────────────────────────────────

def git(clone, *args):
    # TZ: a Go pseudo-version carries the commit time in UTC.
    env = {**os.environ, "TZ": "UTC"}
    return subprocess.check_output(["git", "-C", clone, *args], text=True, env=env).strip()


def default_branch(repository, label, pin, pin_sha):
    """Head of the default branch of repository. label(clone, commit) is the
    version label of a commit. A blobless clone without checkout: history and
    tags, file contents only when label asks for them."""
    with tempfile.TemporaryDirectory(prefix="latest-upstream-") as tmp:
        clone = f"{tmp}/repo"
        subprocess.check_call(["git", "clone", "-q", "--filter=blob:none", "--no-checkout", repository, clone])
        head = git(clone, "rev-parse", "HEAD")
        branch = git(clone, "rev-parse", "--abbrev-ref", "HEAD")
        latest = pin if head == pin_sha else label(clone, head)
        notes = []
        tags = [t for t in git(clone, "tag", "--list").split() if key(t) is not None]
        if tags:
            tag = newest(tags)
            notes.append(f"newest release tag {tag} ({git(clone, 'log', '-1', '--format=%cs', tag)})")
        else:
            notes.append("no release tags")
        notes.append(f"head {head[:7]} of {branch} ({git(clone, 'log', '-1', '--format=%cs', head)})")
        return latest, head, f"{branch} of {repository.removeprefix('https://github.com/').removesuffix('.git')}", "; ".join(notes)


def describe(clone, commit):
    """git describe without the abbreviated commit: 2.3-588 for v2.3-588-gd8820ef."""
    out = git(clone, "describe", "--tags", commit)
    match = re.fullmatch(r"[vV]?(.+?)(?:-(\d+)-g[0-9a-f]+)?", out)
    return match.group(1) + (f"-{match.group(2)}" if match.group(2) else "")


def crate_version(clone, commit):
    """rseip's crate version and the commits since it was set: 0.3.1-2."""
    cargo = git(clone, "show", f"{commit}:Cargo.toml")
    version = re.search(r'^version\s*=\s*"([^"]+)"', cargo, re.M).group(1)
    bump = git(clone, "log", "-1", "--format=%H", f"-Sversion = \"{version}\"", commit, "--", "Cargo.toml")
    count = int(git(clone, "rev-list", "--count", f"{bump}..{commit}"))
    return version + (f"-{count}" if count else "")


def assembly_version(clone, commit):
    """EEIP.NET's AssemblyVersion and the commit: 1.6.0-g58b91a0."""
    info = git(clone, "show", f"{commit}:EEIP.NET/Properties/AssemblyInfo.cs")
    version = re.search(r'^\[assembly: AssemblyVersion\("([\d.]+?)(?:\.\*)?"\)\]', info, re.M).group(1)
    return f"{version}-g{commit[:7]}"


def pseudo_version(clone, commit):
    """Go pseudo-version without the v and with a 7-hex hash: 0.0.0-20221229063210-535f797."""
    stamp = git(clone, "log", "-1", "--date=format-local:%Y%m%d%H%M%S", "--format=%cd", commit)
    return f"0.0.0-{stamp}-{commit[:7]}"


def opener(pin, pin_sha):
    return default_branch("https://github.com/EIPStackGroup/OpENer.git", describe, pin, pin_sha)


def eipscanner(pin, pin_sha):
    return default_branch("https://github.com/nimbuscontrols/EIPScanner.git", describe, pin, pin_sha)


def etherip(pin, pin_sha):
    return default_branch("https://github.com/ornl-epics/etherip.git", describe, pin, pin_sha)


def eiprs(pin, pin_sha):
    return default_branch("https://github.com/Joylei/eip-rs.git", crate_version, pin, pin_sha)


def eeip(pin, pin_sha):
    return default_branch("https://github.com/rossmann-engineering/EEIP.NET.git", assembly_version, pin, pin_sha)


def lokieip(pin, pin_sha):
    return default_branch("https://github.com/loki-os/go-ethernet-ip.git", pseudo_version, pin, pin_sha)


RELEASES = {
    "libplctag": libplctag,
    "gologix": gologix,
    "cpppo": cpppo,
    "pycomm3": pycomm3,
    "pylogix": pylogix,
    "nodeeip": nodeeip,
}
COMMITS = {
    "opener": opener,
    "eipscanner": eipscanner,
    "etherip": etherip,
    "eiprs": eiprs,
    "eeip": eeip,
    "lokieip": lokieip,
}


def arg(dockerfile, name):
    match = re.search(rf"^ARG {name}=(.+)$", dockerfile, re.M)
    if not match:
        raise SystemExit(f"the Dockerfile has no ARG {name}=<pin>")
    return match.group(1).strip()


def main():
    adapters = {**RELEASES, **COMMITS}
    if len(sys.argv) != 2 or sys.argv[1] not in adapters:
        raise SystemExit(f"usage: latest-upstream.py {'|'.join(adapters)}")
    adapter = sys.argv[1]
    dockerfile = (ROOT / "adapters" / adapter / "Dockerfile").read_text()
    pin, pin_sha = arg(dockerfile, "UPSTREAM_VERSION"), arg(dockerfile, "UPSTREAM_SHA")
    if adapter in COMMITS:
        latest, latest_sha, source, note = COMMITS[adapter](pin, pin_sha)
        update = latest_sha != pin_sha
    else:
        latest, latest_sha, source, note = RELEASES[adapter]()
        update = latest != pin
    print(f"pin={pin}")
    print(f"pin_sha={pin_sha}")
    print(f"latest={latest}")
    print(f"latest_sha={latest_sha}")
    print(f"update={'yes' if update else 'no'}")
    print(f"source={source}")
    print(f"note={note}")


if __name__ == "__main__":
    main()
