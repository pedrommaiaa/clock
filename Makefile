SHELL := /bin/bash
.DEFAULT_GOAL := all

# Go binary tools
GO_TOOLS := clock llm act guard aply vrfy rfrsh jolt budg anch risk exec dect graf trce mcp \
	q work dock eval aprv push mem lease mode \
	swrm role hub shrd knox link pbk note rank judge ablt repl farm sync orch \
	self diag spec forge bench gate prom roll audit lmux rcon keys mset chat

BINDIR := bin
GOFLAGS := -trimpath -ldflags="-s -w"

# Shell script tools
SHELL_TOOLS := scan scope srch slce map ctrt flow doss pack undo rpt tick job watch test

.PHONY: all clean install doctor go-tools shell-tools

all: go-tools shell-tools

go-tools: $(addprefix $(BINDIR)/,$(GO_TOOLS))

$(BINDIR)/%: cmd/%/main.go
	@mkdir -p $(BINDIR)
	go build $(GOFLAGS) -o $@ ./cmd/$*

shell-tools:
	@mkdir -p $(BINDIR)
	@for t in $(SHELL_TOOLS); do \
		if [ -f scripts/$$t.sh ]; then \
			cp scripts/$$t.sh $(BINDIR)/$$t && chmod +x $(BINDIR)/$$t; \
		fi; \
	done

install: all
	@echo "Installing Clock tools to /usr/local/bin/clock-*"
	@for f in $(BINDIR)/*; do \
		name=$$(basename $$f); \
		cp $$f /usr/local/bin/clock-$$name; \
	done

clean:
	rm -rf $(BINDIR)

doctor:
	@echo "Checking dependencies..."
	@command -v go >/dev/null 2>&1 && echo "  go: OK" || echo "  go: MISSING"
	@command -v rg >/dev/null 2>&1 && echo "  rg: OK" || echo "  rg: MISSING"
	@command -v git >/dev/null 2>&1 && echo "  git: OK" || echo "  git: MISSING"
	@command -v jq >/dev/null 2>&1 && echo "  jq: OK" || echo "  jq: MISSING"
	@command -v sed >/dev/null 2>&1 && echo "  sed: OK" || echo "  sed: MISSING"
	@command -v awk >/dev/null 2>&1 && echo "  awk: OK" || echo "  awk: MISSING"
	@command -v sqlite3 >/dev/null 2>&1 && echo "  sqlite3: OK" || echo "  sqlite3: MISSING"
	@echo "Done."

test:
	go test ./...

.PHONY: test
