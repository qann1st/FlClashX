#pragma once
#include <sys/types.h>
struct passwd { char *pw_name; uid_t pw_uid; gid_t pw_gid; };
struct passwd *getpwnam(const char *);
struct passwd *getpwuid(uid_t);
