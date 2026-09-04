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

func main() {
	dsn := "postgres://vpnbuilder:vpnbuilder@localhost:5432/vpnbuilder?sslmode=disable"
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

	// Check if admin exists
	email := "admin@vpnbuilder.local"
	_, err = repos.Admins.GetByEmail(ctx, email)
	if err == nil {
		fmt.Println("Admin already exists:", email)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("admin123"), 10)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to hash: %v\n", err)
		os.Exit(1)
	}

	admin, err := repos.Admins.Create(ctx, store.CreateAdminParams{
		Email:        email,
		PasswordHash: string(hash),
		Role:         pgtype.Text{String: "superadmin", Valid: true},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create admin: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Admin created successfully! ID:", admin.ID, "Email:", admin.Email)
}
