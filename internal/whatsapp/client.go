package whatsapp

import (
	"context"
	"fmt"
	"os"

	"github.com/mdp/qrterminal/v3"
	"github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
	_ "modernc.org/sqlite"
)

// Client wraps whatsmeow.Client with setup, QR authentication, and connection lifecycle.
type Client struct {
	WAClient  *whatsmeow.Client
	Container *sqlstore.Container
}

// NewClient initializes a whatsmeow WhatsApp client using SQLite session store.
func NewClient(ctx context.Context, sessionDBPath string, logLevel string) (*Client, error) {
	dbLog := waLog.Stdout("Database", logLevel, true)
	clientLog := waLog.Stdout("Client", logLevel, true)

	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", sessionDBPath)
	container, err := sqlstore.New(ctx, "sqlite", dsn, dbLog)
	if err != nil {
		return nil, fmt.Errorf("failed to open whatsmeow session store: %w", err)
	}

	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		_ = container.Close()
		return nil, fmt.Errorf("failed to get first device store: %w", err)
	}

	waClient := whatsmeow.NewClient(deviceStore, clientLog)

	return &Client{
		WAClient:  waClient,
		Container: container,
	}, nil
}

// Connect starts the connection and displays the QR code in the terminal if unauthenticated.
func (c *Client) Connect(ctx context.Context) error {
	if c.WAClient.Store.ID == nil {
		// Device not logged in, request QR channel
		qrChan, _ := c.WAClient.GetQRChannel(ctx)
		err := c.WAClient.Connect()
		if err != nil {
			return fmt.Errorf("failed to connect client: %w", err)
		}

		go func() {
			for evt := range qrChan {
				if evt.Event == "code" {
					fmt.Println("\n=======================================================")
					fmt.Println("📱 SCAN QR CODE WHATSAPP (Format Ringkas)")
					fmt.Println("=======================================================")
					
					// Format terminal yang ringkas dan pas di layar
					qrConfig := qrterminal.Config{
						Level:      qrterminal.L,
						Writer:     os.Stdout,
						HalfBlocks: true,
						QuietZone:  1,
					}
					qrterminal.GenerateWithConfig(evt.Code, qrConfig)

					// Simpan juga file qrcode.png sebagai alternatif jika terminal terlalu sempit
					pngPath := "qrcode.png"
					if err := qrcode.WriteFile(evt.Code, qrcode.Medium, 256, pngPath); err == nil {
						fmt.Println("-------------------------------------------------------")
						fmt.Printf("💡 Tips: QR code juga disimpan di file: %s\n", pngPath)
						fmt.Println("   Kamu bisa buka file tersebut jika ingin scan dari layar.")
					}
					fmt.Println("=======================================================")
				} else {
					fmt.Printf("Login WhatsApp event: %s\n", evt.Event)
				}
			}
		}()
	} else {
		// Already logged in
		err := c.WAClient.Connect()
		if err != nil {
			return fmt.Errorf("failed to reconnect client: %w", err)
		}
		fmt.Printf("WhatsApp client connected! ID: %s\n", c.WAClient.Store.ID.String())
	}

	return nil
}

// Disconnect gracefully disconnects WhatsApp and closes session storage.
func (c *Client) Disconnect() {
	if c.WAClient != nil && c.WAClient.IsConnected() {
		c.WAClient.Disconnect()
	}
	if c.Container != nil {
		_ = c.Container.Close()
	}
	fmt.Println("WhatsApp client disconnected.")
}
