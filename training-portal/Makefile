all:

sync:
	uv sync

.PHONY: src/static

src/static:
	uv run python src/manage.py collectstatic --no-input

build-project: sync src/static

build-migrations:
	uv run python src/manage.py makemigrations

clean-project:
	rm -rf src/project/__pycache__ src/project/*/__pycache__
	rm -rf src/project/*/*/__pycache__ src/project/*/*/*/__pycache__
	rm -f data/db.sqlite3 data/secret-key.txt
	rm -rf src/static
	rm -rf .venv
