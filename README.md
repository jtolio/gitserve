gitsubmit
=========

A fake SSH server and library for supporting full git repository one-way pushes.

### A sample interaction

Start the server:
```shell
~$ go get github.com/jtolds/gitsubmit
~$ ssh-keygen -N '' -f gitsubmit-key
~$ gitsubmit --addr :7022 --private_key gitsubmit-key \
       --subproc $GOPATH/src/github.com/jtolds/gitsubmit/submission-trigger.py
2014/08/16 02:11:07 NOTE - listening on [::]:7022
```

Push a git repo:
```shell
~$ mkdir myrepo && cd myrepo
~/myrepo$ git init
~/myrepo$ git remote add gitsubmit ssh://localhost:7022/myrepo
~/myrepo$ touch newfile{1,2}
~/myrepo$ git add .
~/myrepo$ git commit -m 'first commit!'
~/myrepo$ git push gitsubmit master
Welcome to the 'gitsubmit' code repo submission tool!
Please see https://github.com/jtolds/gitsubmit for more info.

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

Make sure to check out `submission-trigger.py` to see how to customize `gitsubmit` for your own ends!

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
