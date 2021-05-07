all: vet test

clean:
	rm -rf gen

generate: clean
	mkdir -p gen; cd gen; go run ../cmd/generate-fix/generate-fix.go ../spec/*.xml

generate_maps: generate
	cd gen
	$(if $(shell which stringer-augmented),,$(error "No stringer-augmented in PATH, install from gopaca))
	cd enum && stringer-augmented -type `grep type enums.generated.go | awk '{print $$2}' | xargs | sed -e 's/ /,/g'` -sqlfile enums-generated-maps.sql -output enums.generated-reverse.go enums.generated.go

generate-dist:
	go run cmd/generate-fix/generate-fix.go spec/*.xml

generate-dist-win:
	go run cmd/generate-fix/generate-fix.go spec/FIX42.xml spec/FIX44.xml

fmt:
	go fmt `go list ./... | grep -v quickfix/gen`

vet:
	go vet . ./config ./datadictionary ./enum ./field ./internal ./tag
	go vet ./cmd/generate-fix ./cmd/generate-fix/internal
	go vet ./_test

lint:
	go get golang.org/x/lint/golint
	golint .

test: 
	go test -v -cover . ./datadictionary ./internal

_build_all: 
	go build -v `go list ./...`

build_all_win: 
	go build -v . ./config ./datadictionary ./enum ./field ./fix42 ./fix44 ./internal ./tag ./cmd/generate-fix ./cmd/generate-fix/internal
	cd fix42; go build -v `go list ./...`
	cd fix44; go build -v `go list ./...`

build_accept: 
	cd _test; go build -o echo_server

build: _build_all build_accept

fix40:
	cd _test; ./runat.sh $@.cfg 5001 "definitions/server/$@/*.def"
fix41:
	cd _test; ./runat.sh $@.cfg 5002 "definitions/server/$@/*.def"
fix42:
	cd _test; ./runat.sh $@.cfg 5003 "definitions/server/$@/*.def"
fix43:
	cd _test; ./runat.sh $@.cfg 5004 "definitions/server/$@/*.def"
fix44:
	cd _test; ./runat.sh $@.cfg 5005 "definitions/server/$@/*.def"
fix50:
	cd _test; ./runat.sh $@.cfg 5006 "definitions/server/$@/*.def"
fix50sp1:
	cd _test; ./runat.sh $@.cfg 5007 "definitions/server/$@/*.def"
fix50sp2:
	cd _test; ./runat.sh $@.cfg 5008 "definitions/server/$@/*.def"

ACCEPT_SUITE=fix40 fix41 fix42 fix43 fix44 fix50 fix50sp1 fix50sp2 
accept: $(ACCEPT_SUITE)

.PHONY: test $(ACCEPT_SUITE)
