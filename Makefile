.PHONY: test_e2e
test_e2e:
	go test ./... -v -tags e2e
