SHELL := /bin/bash

.PHONY: help

help:
	@echo 'The Go core and Rust helper build automatically through the setup build'
	@echo 'hook during flutter build; see .agents/commands.md. The core is the'
	@echo 'Elysium-Core fork vendored in core/third_party; tool/sync_core.sh'
	@echo 'copies it from the working checkout.'
