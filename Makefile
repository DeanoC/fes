.PHONY: fmt test vet check

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './.git/*')

test:
	go test -race ./...

vet:
	go vet ./...

check: fmt test vet
