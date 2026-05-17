.PHONY: all build test clean run-demo lint doctor compat

all: build

build:
	@echo "==> Building otelc compiler wrapper..."
	go build -o bin/otelc.exe ./cmd/otelc
	@echo "==> Building supporting tooling..."
	go build -o bin/rule-validator.exe ./cmd/rule-validator
	go build -o bin/hook-generator.exe ./cmd/hook-generator
	go build -o bin/compat-checker.exe ./cmd/compat-checker
	go build -o bin/ast-inspector.exe ./cmd/ast-inspector
	@echo "==> All binaries successfully compiled in otelc-next/bin/"

test:
	@echo "==> Running core engine unit tests..."
	go test -v ./internal/...

clean:
	@echo "==> Cleaning built binaries..."
	rm -rf bin/
	rm -f compatibility-report.md

doctor: build
	@echo "==> Executing otelc system diagnostics..."
	./bin/otelc.exe doctor

compat: build
	@echo "==> Running compatibility checker..."
	./bin/compat-checker.exe

run-demo: build
	@echo "==> Compiling and running microservice demo with AUTOMATIC compile-time instrumentation..."
	# We compile using our newly built otelc compiler wrapper interceptor!
	./bin/otelc.exe build -o bin/microservice-demo.exe ./examples/microservice-demo
	@echo "==> Starting instrumented application in self-test mode..."
	OTELC_TEST_RUN=true ./bin/microservice-demo.exe
