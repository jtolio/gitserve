#!/usr/bin/python -u
#
# Copyright (C) 2014 JT Olds
# See LICENSE for copying information
#

import sys
import time
import shutil
import argparse
import tempfile
from subprocess import check_call, check_output, STDOUT

parser = argparse.ArgumentParser()
parser.add_argument('--repo')
parser.add_argument('--user')
parser.add_argument('--remote')
parser.add_argument('--key')
parser.add_argument('--name')
parser.add_argument('--tags')
args = parser.parse_args()

tags = args.tags.split("\x00")

print
print "Thanks for pushing some code!"
print "==============================================================="
print "You are user: %s" % args.user
print "You pushed to repo: %s" % args.repo
print "You came from: %s" % args.remote
print "The repo name is: %s" % args.name
print "Your public key is: %s..." % args.key[:40]
print "Tags pushed: %s..." % ", ".join(tags)
print

if tags:
  print "You pushed:"
  try:
    worktree = tempfile.mkdtemp()
    # git ls-tree -r is probably better than doing a checkout and then a find,
    # but for demonstrative purposes, showing users how to get the working tree
    # on disk seems useful.
    check_output(["git", "--git-dir", args.repo, "--work-tree", worktree,
                "checkout", "-f", tags[0]], stderr=STDOUT)
    check_call(["find", worktree, "-printf", "./%P\n"])
  finally:
    shutil.rmtree(worktree)

  print
