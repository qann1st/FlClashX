package fake

import (
	"encoding/base64"
	"strings"
)

// Built-in fake payloads, so a config needs no external .bin files.
//
// builtinDiscordUDP is the 1200-byte QUIC v1 Initial that
// zapret-discord-youtube ships as bin/ACTIVE_DISCORD_UDP.bin (MIT). It is the
// decoy that makes a DPI classify a Discord voice flow as QUIC before the real
// STUN/DTLS packets arrive. "quic" is the same packet.
//
// builtinSTUN and builtinDTLS are minimal valid handshakes of their kind, for
// deployments where a QUIC Initial is not what the DPI expects.
const (
	builtinDiscordUDP = "xQAAAAEUVh93DRgo0u2XGqyFS2TKLaqd+swURITinVcRj8DjEGInusk86Gu9TfMAgAAEfJaoPPDyvT9zJbEiVwMGIw5Ys60gJn9QJ7/kp0KlkYWHOTFqiEucEyzVtRucADsK3x3CDdqJ8ldDvM5t13R9H0UiXhvBlhwPblimMjr7er2VOwnW4VhfgtiMmQeCB3ggCSFxwsTwonqDGjDyiV83LFNNlAVT5XkVa8SIjaNqNnLaiGY5+T6CKv/yA1Pi4VjaAGpI1Wil4sWxzBRmE3HHST3BIbCtmmcmpGCGbSaxnEUF1llZE15KbLx99uFy35osxJoC1/eoiuQJs85kOZWaSIHFaEfRxsPvpl4GXX/9mZ1JR14onVHcnMqOuF2yEULizWC6oxJ/J7kb6SjLyrbOUlM12SmiSEjpSM1pYuEy/3BKZK1qrQTftVsIlnzVEmCJhGmckXdaE3BU7pZ3pV8oPaGAVQ52wWhk9RBhDT+w0P1C7ZtyoPrrfidM18FqRl3xqQwrpkDhog/PokKqTu/rBo/rtIQYGEkOipxBNj/9FIOnni/+tM1efJtKYPa0uPa2/xCHHQuAgXTwlcawv/NmzRsV9e+1a1yk/vereIcqhya0UGc7Gn+vCmYjYdjD53doAgkmkR5H79zwbWbFMDI0J1kLJvi6w8XQwSAVHKcpB9/TTOXgd2+BnTchF5jSWGUrtPAbbdzp15kJtGKtz9w9xuj7VV2h7a934p/eKOIOdrXoVmKeH5cscGrHrNvvDCsr9brA00Z7vNmslNzBHWnagi5pXYBdaCc/JKTR+4sbpDe1a0vq4WXKOcACJ+9VJilDoRbgfEY7WazCSRsnqQgukZx0kTA8PqrtHZ3itvSYBLbd5toRdAYBFPyPsB7pszksZGwbqxRJCWx55txwXJl9FQ+cyKfawpC2BXDLbXRs6SQuew2AmajY7h5ZhYRxQgRSLDGYpjeQOvkF0mPi5dLBiHuoQX5gBHL5KWopjq6GlFusBNoX7w1zzrgc+J1VMvMCZ7Vdi2KwgOn2noKNfgVvHHxvCI6kKGpWRlH2mXXR3jF4k+q3Is3Am0Qm0WnMkMmnYsvWvnE3u1aZmwq8tNfSAK3q/myc1G62EsSersCx/AzpcFQbRecIuerm8dM9CsTtTVjCY4ZefCrfnh56RvNQA4O0R/Jt2ziZJL6qpGq2rHpJ/1mfL8QN4zSYPLiQ96pi1Sjon9XnXkdNO07klaEzVYGw4P1dA99a7QgpWtLJWim/VIYidAqlwFRGaSz49llb3veFWUSxgi1WXzel0z3XluNYt3Q/MnXU23C/YA1O90n3YFuCUJuNxK/8LDw8KAKSAh9HDcF7WlR/aEdHNu6MGY/sHj9RhqlhbtDGlkPyxhj1sZnw1Qon6XduojT/hpf8V/LSVqZ6j9wfJr2ikdZtWvebCdyStA5wUPP3px9q7oc4dt77sUZjGv3q7+8t5rDq1nSWtP2X/xMfngU4VVLLf+GMU0oOct0aocTLkDVxvl75FSFulQg5FDZSYtDScfw9ZYI7qoLd7zguGEXM/MyPgGT4BOEGwvJ7QsaEX1Nq4/F7F0bY6HS1touHSVGx"
	builtinSTUN       = "AAEAACESpEIAAAAAAAAAAAAAAAA="
	builtinDTLS       = "Fv79AAAAAAAAAAAAYgEAAFYAAAAAAAAAVv79AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAACwCsBAAAqAA4ABwAEAAEABwAAFwAAAAoABAACAB0ACwACAQAADQAEAAIEA/8BAAEA"
	// TLS ClientHello captured by zapret-discord-youtube (MIT), SNI
	// www.google.com and www.microsoft.com.
	builtinTLSGoogle    = "FgMBAqQBAAKgAwNTG1uXNdJx78bAizzAKwMbiq+pkn0u66h1d29m9srMVCCtyCO678EyIeenMm4uWVx3AbBZ62iWgSmeFYTq4Kp2ygAiEwETAxMCwCvAL8ypzKjALMAwwArACcATwBQAnACdAC8ANQEAAjUAAAATABEAAA53d3cuZ29vZ2xlLmNvbQAXAAD/AQABAAAKAA4ADAAdABcAGAAZAQABAQALAAIBAAAjAAAAEAAOAAwCaDIIaHR0cC8xLjEABQAFAQAAAAAAIgAKAAgEAwUDBgMCAwASAAAAMwBrAGkAHQAgc9SrNzRkNyVefiBmQR2P18zGZEMhns39hokBKFzEXX8AFwBBBNorhDIiHjDbOxxivaYzI7xFWn577hf1dTS3ODhb/k3UxEB8lo2gLZ06wP5KkAL9V7X6U86Xaon5v0ZCkn4fAYUAKwAJCAMEAwMDAgMBAA0AGAAWBAMFAwYDCAQIBQgGBAEFAQYBAgMCAQAtAAIBAQAcAAJAAQAbAAcGAAEAAgAD/g0BGQAAAQADyQAg1tH5QJXQtpGsnLZ4sl3WzXjKncSRlI0PwQMvsrUduz0A7+jN/qbb6Q5HuiaQqvST/oy6dLwuum7WYehHi1EyrpAdTQt/rMO4TdE4G/69LMtoWuVGvwx3ktK9d7cysemsULSnXn925UTNxExeMxXRYmYfsTKySAwL9EhfNNI+By6BWCPqkmwuLSMcbvPgeo8l2gCcRvAG7IxWFkCQ87a6bflXx26x/EpfQ27LP69kZedx/BmTn4YjB+uYwTr6H105Sop8MxMpTMztwbhy3kTi1WIEiDScrdMl9ovTstmTj9Hb3EpUBk2qrgVb0Z/8vvmt+TA2A1/dCCIJFH7gGKfNtib7S6Lc6+te2MKDoMqJGmt7"
	builtinTLSMicrosoft = "FgMBAqMBAAKfAwNBiIItT/2BSJ7nkGUfugV7/6da+VuKj0WLQfA9G93j+CCbI6XSIR6f54Vs/GGAOj+6uWC6sw6YJ2z3OChlgF1AOAAiEwETAxMCwCvAL8ypzKjALMAwwArACcATwBQAnACdAC8ANQEAAjQAAAAWABQAABF3d3cubWljcm9zb2Z0LmNvbQAXAAD/AQABAAAKAA4ADAAdABcAGAAZAQABAQALAAIBAAAjAAAAEAAOAAwCaDIIaHR0cC8xLjEABQAFAQAAAAAAIgAKAAgEAwUDBgMCAwASAAAAMwBrAGkAHQAgaRUWKW2t1WiIJy/er6w8TKTk2Mj7QYf0dk4O+mTE6SkAFwBBBP5iuQjIwyq5hzeEQmtczcnKYjjT2ZmKxC3G0KNgshJUQY5SXuOr+cIHgdz48mqRQC/LpP9vJMdNd3ctb+B3qpIAKwAFBAMEAwMADQAYABYEAwUDBgMIBAgFCAYEAQUBBgECAwIBAC0AAgEBABwAAkABABsABwYAAQACAAP+DQEZAAABAAMhACBi6IPYlwWKvqHyY07Ok4SOz+fdsuSHBqwRGb4OcYfxpgDv2GsnXsCnXUJOjNzznxxRYu//W+3I/e5vu4ibsTCcZkKrD2aJGIsRwW3nKuuWO39SeNv4bQT3lRqo8GRSBznwqB0NFja3GA7IRCf+8zHw3ox09aHYj29Fl2l5Xi7UsCwMGm/MzpDH3cZglfPCGd5QgL/e8iVjFSZjCR/F3zL16pzS/5lOZ6LlGpSF4982pYNLChyv10jJS4on3Vh/lfJr3isS0+xNaTecE5sWsEVSOHdp76plGbzCk02wG39bQf+vulBRw/EnCSX1YJAJseXAx0J4VDsjGX2OchO0081jtsRKKD1FPovbhE94ZDBp4hs="
)

var builtinPayloads = map[string]string{
	"discord-udp":   builtinDiscordUDP,
	"discord":       builtinDiscordUDP,
	"quic":          builtinDiscordUDP,
	"stun":          builtinSTUN,
	"dtls":          builtinDTLS,
	"tls-google":    builtinTLSGoogle,
	"tls":           builtinTLSMicrosoft,
	"tls-microsoft": builtinTLSMicrosoft,
}

// Builtin returns the bytes of a payload compiled into the core.
func Builtin(name string) ([]byte, bool) {
	b64, ok := builtinPayloads[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return nil, false
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, false
	}
	return data, true
}
