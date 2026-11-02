.PHONY: test lint demo examples

RUN = go run ./plugin/scripts

test:
	test -z "$$(gofmt -l plugin tests examples)"
	go vet ./...
	go test ./...

lint:
	claude plugin validate ./plugin --strict
	claude plugin validate . --strict

# The hotfix comparison has findings on purpose, so it exits 1 with --fail.
demo:
	$(RUN) examples/buildinfo/orders-v1.4.1.txt examples/buildinfo/orders-hotfix.txt
	$(RUN) --fail examples/buildinfo/orders-v1.4.0.txt examples/buildinfo/orders-v1.4.1.txt

# Rebuilds the example build information from examples/orders. Needs git, and
# the network the first time to download the dependencies.
examples:
	scripts/build-examples.sh
