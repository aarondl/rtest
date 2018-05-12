# rtest

Recursive test runner. Runs go test over and over again in the directory
of a changed file. Uses inotify to be efficient.

Usage:

```bash
rtest [rtest-flags] -- [go test flags]
```
