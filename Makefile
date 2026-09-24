.PHONY: up down reset logs test eval

up:
	docker compose up --build -d

down:
	docker compose down

reset:
	docker compose down -v

logs:
	docker compose logs -f

test:
	docker run --rm -v $(PWD)/backend:/src -w /src golang:1.26-alpine go test ./...

# make eval EXPECTED=/ruta/expected_results.csv
eval:
	docker compose run --rm -v $(EXPECTED):/eval/expected_results.csv:ro \
	  --entrypoint eval backend --expected /eval/expected_results.csv
