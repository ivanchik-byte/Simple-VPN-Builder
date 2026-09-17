package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
)

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	dsn := getenv("DATABASE_URL", "postgres://vpnbuilder:vpnbuilder@localhost:5432/vpnbuilder?sslmode=disable")
	email := getenv("SEED_ADMIN_EMAIL", "admin@vpnbuilder.local")
	password := os.Getenv("SEED_ADMIN_PASSWORD")
	role := getenv("SEED_ADMIN_ROLE", "superadmin")
	if password == "" {
		fmt.Fprintln(os.Stderr, "SEED_ADMIN_PASSWORD is required (no default password is ever used)")
		os.Exit(1)
	}

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to connect: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := store.RunMigrations(pool); err != nil {
		fmt.Fprintf(os.Stderr, "failed migrations: %v\n", err)
		os.Exit(1)
	}

	repos := store.NewRepositories(pool)

	_, err = repos.Admins.GetByEmail(ctx, email)
	if err == nil {
		fmt.Println("Admin already exists:", email)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to hash: %v\n", err)
		os.Exit(1)
	}

	admin, err := repos.Admins.Create(ctx, store.CreateAdminParams{
		Email:        email,
		PasswordHash: string(hash),
		Role:         pgtype.Text{String: role, Valid: true},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create admin: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Admin created successfully! ID:", admin.ID, "Email:", admin.Email)
}
