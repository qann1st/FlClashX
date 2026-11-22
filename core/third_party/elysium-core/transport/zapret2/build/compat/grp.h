#pragma once
#include <sys/types.h>
struct group { char *gr_name; gid_t gr_gid; };
struct group *getgrnam(const char *);
int getgroups(int, gid_t []);
int getgrouplist(const char *, gid_t, gid_t *, int *);
