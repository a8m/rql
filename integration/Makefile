test:
	docker build .
	docker-compose up -d --scale integration=0
	docker-compose run integration
	docker-compose down