module simple_db

replace file_manager => ./file_manager

replace log_manager => ./log_manager

go 1.17

require (
	file_manager v0.0.0-00010101000000-000000000000
	log_manager v0.0.0-00010101000000-000000000000
)
