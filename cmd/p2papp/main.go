package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"p2papp/internal/chat"
	"p2papp/internal/cloud"
	"p2papp/internal/core"
	"p2papp/internal/wallet"
)

var (
	dataDir    string
	walletPath string
	cloudDir   string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "p2papp",
		Short: "Decentralized P2P Chat & Cloud Application",
		Long:  "A peer-to-peer application for decentralized chat and cloud file storage using libp2p.",
	}

	// Global flags
	homeDir, _ := os.UserHomeDir()
	defaultDataDir := filepath.Join(homeDir, ".p2papp")

	rootCmd.PersistentFlags().StringVar(&dataDir, "data-dir", defaultDataDir, "Data directory path")

	// Add subcommands
	rootCmd.AddCommand(
		walletCmd(),
		chatCmd(),
		cloudCmd(),
		runCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// ================== WALLET COMMANDS ==================

func walletCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wallet",
		Short: "Wallet management commands",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "create",
			Short: "Create a new wallet",
			RunE:  walletCreate,
		},
		&cobra.Command{
			Use:   "show",
			Short: "Show wallet information",
			RunE:  walletShow,
		},
	)

	return cmd
}

func walletCreate(cmd *cobra.Command, args []string) error {
	walletPath := filepath.Join(dataDir, "wallet.json")

	// Check if wallet exists
	if _, err := os.Stat(walletPath); err == nil {
		fmt.Println("Wallet already exists at:", walletPath)
		fmt.Print("Overwrite? (y/N): ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(answer)) != "y" {
			return nil
		}
	}

	svc := wallet.NewWalletService()
	w, err := svc.Create()
	if err != nil {
		return fmt.Errorf("failed to create wallet: %w", err)
	}

	if err := svc.Save(w, walletPath); err != nil {
		return fmt.Errorf("failed to save wallet: %w", err)
	}

	fmt.Println("✅ Wallet created successfully!")
	fmt.Println("📍 Peer ID:", w.PeerID.String())
	fmt.Println("📁 Saved to:", walletPath)
	return nil
}

func walletShow(cmd *cobra.Command, args []string) error {
	walletPath := filepath.Join(dataDir, "wallet.json")

	svc := wallet.NewWalletService()
	w, err := svc.Load(walletPath)
	if err != nil {
		return fmt.Errorf("failed to load wallet: %w", err)
	}

	fmt.Println("📍 Peer ID:", w.PeerID.String())
	fmt.Println("📁 Wallet path:", walletPath)
	return nil
}

// ================== CHAT COMMANDS ==================

func chatCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Chat commands",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "join [room]",
			Short: "Join a chat room",
			Args:  cobra.ExactArgs(1),
			RunE:  chatJoin,
		},
	)

	return cmd
}

func chatJoin(cmd *cobra.Command, args []string) error {
	roomID := args[0]

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Load wallet
	walletPath := filepath.Join(dataDir, "wallet.json")
	svc := wallet.NewWalletService()
	w, err := svc.Load(walletPath)
	if err != nil {
		return fmt.Errorf("failed to load wallet (run 'wallet create' first): %w", err)
	}

	// Create P2P host
	cfg := core.DefaultConfig()
	cfg.PrivKey = w.PrivKey
	host, err := core.NewP2PHost(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to create P2P host: %w", err)
	}
	defer host.Close()

	fmt.Println("🌐 P2P Host started")
	fmt.Println("📍 Peer ID:", host.ID().String())
	for _, addr := range host.Addrs() {
		fmt.Println("📡", addr)
	}

	// Create chat service
	chatSvc, err := chat.NewChatService(ctx, host)
	if err != nil {
		return fmt.Errorf("failed to create chat service: %w", err)
	}
	defer chatSvc.Close()

	// Join room
	if err := chatSvc.JoinRoom(roomID); err != nil {
		return fmt.Errorf("failed to join room: %w", err)
	}

	fmt.Printf("\n💬 Joined room: %s\n", roomID)
	fmt.Println("Type messages and press Enter to send. Ctrl+C to exit.\n")

	// Handle messages
	go func() {
		for msg := range chatSvc.Subscribe() {
			shortID := msg.From.String()
			if len(shortID) > 8 {
				shortID = shortID[:8]
			}
			fmt.Printf("\n[%s] %s: %s\n> ", msg.Timestamp.Format("15:04:05"), shortID, msg.Content)
		}
	}()

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Read input
	reader := bufio.NewReader(os.Stdin)
	inputChan := make(chan string)

	go func() {
		for {
			fmt.Print("> ")
			text, err := reader.ReadString('\n')
			if err != nil {
				close(inputChan)
				return
			}
			inputChan <- strings.TrimSpace(text)
		}
	}()

	for {
		select {
		case <-sigChan:
			fmt.Println("\n👋 Leaving room...")
			return nil
		case text, ok := <-inputChan:
			if !ok {
				return nil
			}
			if text == "" {
				continue
			}
			if err := chatSvc.SendMessage(roomID, text); err != nil {
				fmt.Printf("Error sending message: %v\n", err)
			}
		}
	}
}

// ================== CLOUD COMMANDS ==================

func cloudCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cloud",
		Short: "Cloud storage commands",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "upload [file]",
			Short: "Upload a file",
			Args:  cobra.ExactArgs(1),
			RunE:  cloudUpload,
		},
		&cobra.Command{
			Use:   "download [hash] [dest]",
			Short: "Download a file by hash",
			Args:  cobra.ExactArgs(2),
			RunE:  cloudDownload,
		},
		&cobra.Command{
			Use:   "list",
			Short: "List uploaded files",
			RunE:  cloudList,
		},
	)

	return cmd
}

func cloudUpload(cmd *cobra.Command, args []string) error {
	filePath := args[0]

	ctx := context.Background()

	// Load wallet
	walletPath := filepath.Join(dataDir, "wallet.json")
	svc := wallet.NewWalletService()
	w, err := svc.Load(walletPath)
	if err != nil {
		return fmt.Errorf("failed to load wallet: %w", err)
	}

	// Create P2P host
	cfg := core.DefaultConfig()
	cfg.PrivKey = w.PrivKey
	host, err := core.NewP2PHost(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to create P2P host: %w", err)
	}
	defer host.Close()

	// Create cloud service
	cloudDir := filepath.Join(dataDir, "cloud")
	cloudSvc, err := cloud.NewCloudService(ctx, host, cloudDir)
	if err != nil {
		return fmt.Errorf("failed to create cloud service: %w", err)
	}
	defer cloudSvc.Close()

	// Upload file
	info, err := cloudSvc.Upload(filePath)
	if err != nil {
		return fmt.Errorf("failed to upload file: %w", err)
	}

	fmt.Println("✅ File uploaded successfully!")
	fmt.Println("📁 Name:", info.Name)
	fmt.Println("📊 Size:", formatBytes(info.Size))
	fmt.Println("🔗 Hash:", info.Hash)
	fmt.Println("📦 Chunks:", info.ChunkCount)
	return nil
}

func cloudDownload(cmd *cobra.Command, args []string) error {
	hash := cloud.FileHash(args[0])
	destPath := args[1]

	ctx := context.Background()

	// Load wallet
	walletPath := filepath.Join(dataDir, "wallet.json")
	svc := wallet.NewWalletService()
	w, err := svc.Load(walletPath)
	if err != nil {
		return fmt.Errorf("failed to load wallet: %w", err)
	}

	// Create P2P host
	cfg := core.DefaultConfig()
	cfg.PrivKey = w.PrivKey
	host, err := core.NewP2PHost(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to create P2P host: %w", err)
	}
	defer host.Close()

	// Create cloud service
	cloudDir := filepath.Join(dataDir, "cloud")
	cloudSvc, err := cloud.NewCloudService(ctx, host, cloudDir)
	if err != nil {
		return fmt.Errorf("failed to create cloud service: %w", err)
	}
	defer cloudSvc.Close()

	// Download file
	fmt.Println("⏳ Downloading...")
	if err := cloudSvc.Download(hash, destPath); err != nil {
		return fmt.Errorf("failed to download file: %w", err)
	}

	fmt.Println("✅ File downloaded to:", destPath)
	return nil
}

func cloudList(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Load wallet
	walletPath := filepath.Join(dataDir, "wallet.json")
	svc := wallet.NewWalletService()
	w, err := svc.Load(walletPath)
	if err != nil {
		return fmt.Errorf("failed to load wallet: %w", err)
	}

	// Create P2P host
	cfg := core.DefaultConfig()
	cfg.PrivKey = w.PrivKey
	host, err := core.NewP2PHost(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to create P2P host: %w", err)
	}
	defer host.Close()

	// Create cloud service
	cloudDir := filepath.Join(dataDir, "cloud")
	cloudSvc, err := cloud.NewCloudService(ctx, host, cloudDir)
	if err != nil {
		return fmt.Errorf("failed to create cloud service: %w", err)
	}
	defer cloudSvc.Close()

	// List files
	files, err := cloudSvc.List()
	if err != nil {
		return fmt.Errorf("failed to list files: %w", err)
	}

	if len(files) == 0 {
		fmt.Println("No files stored.")
		return nil
	}

	fmt.Println("📁 Stored files:\n")
	for _, f := range files {
		fmt.Printf("  %s\n", f.Name)
		fmt.Printf("    Hash: %s\n", f.Hash)
		fmt.Printf("    Size: %s (%d chunks)\n\n", formatBytes(f.Size), f.ChunkCount)
	}
	return nil
}

// ================== RUN COMMAND (Interactive) ==================

func runCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run interactive P2P node",
		RunE:  runInteractive,
	}
}

func runInteractive(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Load wallet
	walletPath := filepath.Join(dataDir, "wallet.json")
	svc := wallet.NewWalletService()
	w, err := svc.Load(walletPath)
	if err != nil {
		return fmt.Errorf("failed to load wallet (run 'wallet create' first): %w", err)
	}

	// Create P2P host
	cfg := core.DefaultConfig()
	cfg.PrivKey = w.PrivKey
	host, err := core.NewP2PHost(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to create P2P host: %w", err)
	}
	defer host.Close()

	fmt.Println("🚀 P2P Node started!")
	fmt.Println("📍 Peer ID:", host.ID().String())
	fmt.Println("\n📡 Listening addresses:")
	for _, addr := range host.Addrs() {
		fmt.Println("  ", addr)
	}

	// Wait for interrupt
	fmt.Println("\nPress Ctrl+C to stop the node.")
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\n👋 Shutting down...")
	return nil
}

// ================== HELPERS ==================

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
