SHELL := /bin/bash

.PHONY: help

help:
	@echo 'The Go core and Rust helper build automatically through the setup build'
	@echo 'hook during flutter build; see .agents/commands.md. The core (a'
	@echo 'Prizrak-Core fork) comes from the Go module cache (core/go.mod), there is'
	@echo 'no submodule to update.'
