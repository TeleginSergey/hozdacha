package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	if len(os.Args) != 4 {
		log.Fatal("Usage: create_admin <username> <email> <password>")
	}

	username := os.Args[1]
	email := os.Args[2]
	password := os.Args[3]

	// Database connection
	dbHost := os.Getenv("DB_HOST")
	if dbHost == "" {
		dbHost = "db"
	}
	dbPort := os.Getenv("DB_PORT")
	if dbPort == "" {
		dbPort = "5432"
	}
	dbUser := os.Getenv("DB_USER")
	if dbUser == "" {
		dbUser = "postgres"
	}
	dbPassword := os.Getenv("DB_PASSWORD")
	if dbPassword == "" {
		dbPassword = "postgres"
	}
	dbName := os.Getenv("DB_NAME")
	if dbName == "" {
		dbName = "hozdacha"
	}

	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		dbHost, dbPort, dbUser, dbPassword, dbName)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatal("Failed to ping database:", err)
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Fatal("Failed to hash password:", err)
	}

	// Insert admin user
	query := `
		INSERT INTO users (username, email, password, role_id, email_verified, created_at, updated_at)
		VALUES ($1, $2, $3, 2, true, NOW(), NOW())
		ON CONFLICT (username) DO UPDATE SET
		email = EXCLUDED.email,
		password = EXCLUDED.password,
		role_id = EXCLUDED.role_id,
		email_verified = EXCLUDED.email_verified,
		updated_at = NOW()
		RETURNING id, username, email, role_id`

	var id int64
	var returnedUsername, returnedEmail string
	var roleID int

	err = db.QueryRow(query, username, email, string(hashedPassword)).Scan(&id, &returnedUsername, &returnedEmail, &roleID)
	if err != nil {
		log.Fatal("Failed to create admin user:", err)
	}

	fmt.Printf("Admin user created successfully!\n")
	fmt.Printf("ID: %d\n", id)
	fmt.Printf("Username: %s\n", returnedUsername)
	fmt.Printf("Email: %s\n", returnedEmail)
	fmt.Printf("Role ID: %d\n", roleID)
	fmt.Printf("Password: %s\n", password)
}
