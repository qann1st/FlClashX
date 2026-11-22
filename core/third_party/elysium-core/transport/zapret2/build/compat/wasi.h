#pragma once
#include <sys/socket.h>
#include <netinet/in.h>
#include <sys/types.h>
#include <errno.h>
#include <signal.h>

#define IPPROTO_HOPOPTS 0
#define IPPROTO_IGMP 2
#define IPPROTO_IPIP 4
#define IPPROTO_ROUTING 43
#define IPPROTO_FRAGMENT 44
#define IPPROTO_ESP 50
#define IPPROTO_AH 51
#define IPPROTO_ICMPV6 58
#define IPPROTO_NONE 59
#define IPPROTO_DSTOPTS 60
#define SIOCGIFMTU 0x8921
#define IPPROTO_RAW 255
