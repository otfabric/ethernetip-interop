#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Tests of scripts/check-versions.sh: on a copy of the repository in which one
build file disagrees with versions.yaml, the check exits 1 and says where."""

import glob
import os
import shutil
import subprocess
import tempfile
import unittest

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
COPY = ("versions.yaml", "Makefile", "scripts", "adapters", "own")


@unittest.skipUnless(shutil.which("bash") and shutil.which("make"), "needs bash and make")
class CheckVersions(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = os.path.join(self.tmp.name, "repo")
        os.mkdir(self.root)
        for name in COPY:
            src = os.path.join(ROOT, name)
            if os.path.isdir(src):
                shutil.copytree(src, os.path.join(self.root, name),
                                ignore=shutil.ignore_patterns("__pycache__", "*.pyc"))
            else:
                shutil.copy2(src, os.path.join(self.root, name))

    def edit(self, path, old, new):
        path = os.path.join(self.root, path)
        with open(path, newline="") as f:
            text = f.read()
        self.assertEqual(text.count(old), 1, "%s: %r" % (path, old))
        with open(path, "w", newline="") as f:
            f.write(text.replace(old, new))

    def check(self):
        p = subprocess.run(["bash", "scripts/check-versions.sh"], cwd=self.root, capture_output=True, text=True)
        return p.returncode, [line for line in p.stdout.splitlines() if line.startswith("FAIL")]

    def assertFails(self, needle):
        code, failed = self.check()
        self.assertEqual(code, 1, failed)
        self.assertTrue(any(needle in line for line in failed), failed)

    def test_the_repository_as_committed_passes(self):
        self.assertEqual(self.check(), (0, []))

    def test_a_go_mod_version_that_the_dockerfile_does_not_build_fails(self):
        self.edit("adapters/gologix/go.mod", "gologix v0.44.0-beta", "gologix v0.43.0")
        self.assertFails("gologix in Dockerfile and go.mod")

    def test_a_pseudo_version_of_another_revision_fails(self):
        self.edit("adapters/lokieip/go.mod", "-535f79752f63", "-535f79752f64")
        self.assertFails("lokieip in Dockerfile and go.mod")

    def test_a_go_sum_hash_that_versions_yaml_does_not_record_fails(self):
        self.edit("adapters/cbsigma/go.sum", "h1:JOXJPLSjbh2crsM+dPthDDHLEXyYarhEjHLyUPlcK3w=",
                  "h1:AAXJPLSjbh2crsM+dPthDDHLEXyYarhEjHLyUPlcK3w=")
        self.assertFails("cbsigma module sum in go.sum and versions.yaml")

    def test_a_base_image_digest_of_own_that_versions_yaml_does_not_record_fails(self):
        (dockerfile,) = glob.glob(os.path.join(self.root, "own", "*", "Dockerfile"))
        rel = os.path.relpath(dockerfile, self.root)
        self.edit(rel, "GOLANG_DIGEST=sha256:b54b", "GOLANG_DIGEST=sha256:0000")
        self.assertFails(rel.replace(os.sep, "/") + " base image digest")

    def test_a_tool_image_without_a_digest_fails(self):
        self.edit("Makefile", "koalaman/shellcheck:v0.11.0@sha256:61862eba1fcf09a484ebcc6feea46f1782532571a34ed51fedf90dd25f925a8d",
                  "koalaman/shellcheck:stable")
        self.assertFails("Makefile SHELLCHECK_IMAGE")

    def test_a_tool_image_that_versions_yaml_does_not_record_fails(self):
        self.edit("scripts/lib.sh", "python:3.12.16-slim@sha256:5e2b", "python:3.12.16-slim@sha256:0000")
        self.assertFails("scripts/lib.sh PYTHON_IMAGE")

    def test_a_govulncheck_version_that_versions_yaml_does_not_record_fails(self):
        self.edit("Makefile", "GOVULNCHECK_VERSION ?= v1.9.0", "GOVULNCHECK_VERSION ?= v1.8.0")
        self.assertFails("Makefile govulncheck")


if __name__ == "__main__":
    unittest.main()
