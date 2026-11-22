#pragma once
#define LOG_ERR 3
#define LOG_DEBUG 7
#define LOG_DAEMON (3 << 3)
#define LOG_USER (1 << 3)
#define LOG_PID 1
#define LOG_PERROR 32
void openlog(const char *, int, int);
void syslog(int, const char *, ...);
void closelog(void);
