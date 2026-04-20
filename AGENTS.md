## Build & Test

```sh
make build          # Build Go sources under src/ → ./roost
make vet            # go vet ./...
make lint           # golangci-lint (depguard, funlen, staticcheck, etc.)
cd src && go test ./...          # Run all tests
cd src && go test ./path/to/pkg  # Run tests for a specific package
cd src && go test -run TestName ./...  # Run a specific test
```

## Rules

- Follow the design principles in ARCHITECTURE.md
- Keep files under 500 lines and functions under 50 lines
- Actively use libraries. Do not implement from scratch
- Do not overwrite user config files (~/.roost/)
- Always write tests for new features and bug fixes. Do not consider work complete without tests
