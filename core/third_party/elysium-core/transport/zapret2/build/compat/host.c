#include "sec.h"
#include <pwd.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <errno.h>

struct passwd *getpwnam(const char *name) { return NULL; }
bool droproot(uid_t uid, const char *name, const gid_t *groups, int count) { return false; }
bool can_drop_root(void) { return false; }
void print_id(void) {}
void openlog(const char *name, int flags, int facility) {}
FILE *tmpfile(void) { errno = ENOSYS; return NULL; }
char *tmpnam(char *path) { errno = ENOSYS; return NULL; }
int system(const char *command) { errno = ENOSYS; return -1; }
void syslog(int priority, const char *fmt, ...)
{
	va_list args;
	va_start(args, fmt);
	vfprintf(stderr, fmt, args);
	va_end(args);
}
