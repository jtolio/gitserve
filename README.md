gitserve
=========

A restricted SSH server and library for supporting controlled Git repository
access and code submission.

This library comes with two tools:
 * `git-submitd`: A service that supports one-way pushes of full repos, along
      with submission hooks for inspecting and accepting those repos.
 * `git-hostd`: A service that hosts a folder of git repos to users that have
      ssh keys in a specific whitelist.

### git-submitd sample interaction

Start the server:
```shell
~$ go get github.com/jtolds/gitserve/cmd/git-submitd
~$ ssh-keygen -N '' -qf git-submitd-key
~$ git-submitd --addr :7022 --private_key git-submitd-key \
       --subproc $GOPATH/src/github.com/jtolds/gitserve/cmd/git-submitd/submission-trigger.py
2014/08/16 02:11:07 NOTE - listening on [::]:7022
```

Push a git repo:
```shell
~$ mkdir myrepo && cd myrepo
~/myrepo$ git init
Initialized empty Git repository in /home/jt/myrepo/.git/
~/myrepo$ git remote add git-submitd ssh://localhost:7022/myrepo
~/myrepo$ touch newfile{1,2}
~/myrepo$ git add .
~/myrepo$ git commit -m 'first commit!'
[master (root-commit) 2266e76] first commit!
 0 files changed
 create mode 100644 newfile1
 create mode 100644 newfile2
~/myrepo$ git push git-submitd master
Welcome to the gitserve git-submitd code repo submission tool!
Please see https://github.com/jtolds/gitserve for more info.

Counting objects: 3, done.
Delta compression using up to 4 threads.
Compressing objects: 100% (2/2), done.
Writing objects: 100% (3/3), 218 bytes, done.
Total 3 (delta 0), reused 0 (delta 0)

Thanks for pushing some code!
===============================================================
You are user: jt
You pushed repo: /tmp/submission-907291030
You came from: [::1]:39059
The repo name is: /myrepo
Your public key is: ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDB...

You pushed:
/tmp/tmpRM4PbC
/tmp/tmpRM4PbC/newfile1
/tmp/tmpRM4PbC/newfile2

To ssh://localhost:7022/myrepo
 * [new branch]      master -> master
~/myrepo$
```

Make sure to check out `submission-trigger.py` to see how to customize
git-submitd for your own ends!

### git-hostd sample interaction

Make a repo and start the server:
```shell
~$ mkdir -p server/myrepo && cd server/myrepo
~/server/myrepo$ git init
Initialized empty Git repository in /home/jt/server/myrepo/.git/
~/server/myrepo$ touch newfile1
~/server/myrepo$ git add .
~/server/myrepo$ git commit -m 'first commit!'
[master (root-commit) 2266e76] first commit!
 0 files changed
 create mode 100644 newfile1
~/server/myrepo$ cd -
~$ go get github.com/jtolds/gitserve/cmd/git-hostd
~$ ssh-keygen -N '' -qf git-hostd-key
~$ cat ~/.ssh/idrsa.pub > git-hostd-authorized
~$ git-hostd --addr :7022 --repo_base server --private_key git-hostd-key \
       --authorized_keys git-hostd-authorized
2014/08/16 02:11:07 NOTE - listening on [::]:7022
```

Clone your repo from somewhere else, make a change, and push:
```shell
~$ git clone ssh://localhost:7022/myrepo
Cloning into 'myrepo'...
Welcome to the gitserve git-hostd code hosting tool!
Please see https://github.com/jtolds/gitserve for more info.

remote: Counting objects: 3, done.
remote: Compressing objects: 100% (2/2), done.
remote: Total 3 (delta 0), reused 0 (delta 0)
Receiving objects: 100% (3/3), done.
~$ cd myrepo
~/myrepo$ touch newfile2
~/myrepo$ git add newfile2
~/myrepo$ git commit -m 'second commit!'
[master 043fcab] second commit!
 0 files changed
 create mode 100644 newfile2
~/myrepo$ git push origin HEAD:refs/heads/mybranch
Welcome to the gitserve git-hostd code hosting tool!
Please see https://github.com/jtolds/gitserve for more info.

Counting objects: 3, done.
Delta compression using up to 4 threads.
Compressing objects: 100% (2/2), done.
Writing objects: 100% (2/2), 230 bytes, done.
Total 2 (delta 0), reused 0 (delta 0)
To ssh://localhost:7022/myrepo
 * [new branch]      HEAD -> mybranch
~/myrepo$
```

#### License

```plain
The MIT License (MIT)

Copyright (c) 2014 JT Olds

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```
