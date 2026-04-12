## Build & Test

```sh
make build          # Build Go sources under src/ → ./roost
make vet            # go vet ./...
cd src && go test ./...  # Run all tests
```

## Rules

- Follow the design principles in ARCHITECTURE.md
- Keep files under 500 lines and functions under 50 lines
- Actively use libraries. Do not implement from scratch
- Do not overwrite user config files (~/.roost/)
- Always write tests for new features and bug fixes. Do not consider work complete without tests
