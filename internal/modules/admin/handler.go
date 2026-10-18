package admin

import (
	"fmt"
	"strings"

	"whatsapp-bot/internal/database"
	"whatsapp-bot/internal/nlu"
	"whatsapp-bot/internal/router"
)

// Handler handles administrator actions including group whitelisting and admin management.
type Handler struct {
	db *database.DB
}

// NewHandler creates a new admin module handler.
func NewHandler(db *database.DB) *Handler {
	return &Handler{
		db: db,
	}
}

func (h *Handler) Name() string {
	return "admin"
}

func (h *Handler) CanHandle(result *nlu.NLUResult) bool {
	if result == nil {
		return false
	}
	return result.Intent == nlu.IntentAdminCommand
}

func (h *Handler) Execute(ctx *router.MessageContext) (string, error) {
	senderPhone := ctx.SenderPhone
	if senderPhone == "" {
		senderPhone = ctx.SenderJID.User
	}

	// Verify authorization (checks resolved phone number or raw JID user/LID)
	if !h.db.IsAdmin(ctx.Context, senderPhone) && !h.db.IsAdmin(ctx.Context, ctx.SenderJID.User) {
		return "⛔ *Akses Ditolak*\nPerintah pengaturan bot hanya dapat dijalankan oleh admin nomor telepon terdaftar.", nil
	}

	payload := strings.TrimSpace(ctx.NLUResult.Entities.RawPayload)
	lower := strings.ToLower(payload)

	switch {
	case strings.HasPrefix(lower, "izinkan grup") || strings.HasPrefix(lower, "allow group"):
		return h.handleAllowGroup(ctx, payload)
	case strings.HasPrefix(lower, "cabut grup") || strings.HasPrefix(lower, "revoke group"):
		return h.handleRevokeGroup(ctx, payload)
	case strings.HasPrefix(lower, "daftar grup"):
		return h.handleListGroups(ctx)
	case strings.HasPrefix(lower, "tambah admin"):
		return h.handleAddAdmin(ctx, payload)
	case strings.HasPrefix(lower, "hapus admin"):
		return h.handleRemoveAdmin(ctx, payload)
	case strings.HasPrefix(lower, "daftar admin"):
		return h.handleListAdmins(ctx)
	default:
		return "⚠️ Perintah admin tidak dikenali. Gunakan:\n" +
			"• `@Bot izinkan grup`\n" +
			"• `@Bot cabut grup`\n" +
			"• `@Bot daftar grup`\n" +
			"• `@Bot tambah admin <nomor>`\n" +
			"• `@Bot hapus admin <nomor>`\n" +
			"• `@Bot daftar admin`", nil
	}
}

func (h *Handler) handleAllowGroup(ctx *router.MessageContext, payload string) (string, error) {
	var targetJID string
	var groupName string

	fields := strings.Fields(payload)
	if len(fields) >= 3 {
		arg := strings.TrimSpace(fields[2])
		if strings.Contains(arg, "@g.us") {
			targetJID = arg
			groupName = "Grup " + targetJID
		} else if len(arg) > 10 { // format angka id grup WhatsApp
			targetJID = arg + "@g.us"
			groupName = "Grup " + targetJID
		}
	}
	if targetJID == "" && ctx.IsGroup {
		targetJID = ctx.ChatJID.String()
		groupName = "Grup Ini"
	}
	if targetJID == "" {
		return "⚠️ Gunakan perintah ini langsung di dalam grup WhatsApp yang ingin diizinkan, atau tentukan JID grup:\n`@Bot izinkan grup <groupID@g.us>`", nil
	}

	sender := ctx.SenderName
	if sender == "" {
		sender = ctx.SenderJID.User
	}

	if err := h.db.AllowGroup(ctx.Context, targetJID, groupName, sender); err != nil {
		return "", fmt.Errorf("gagal mengizinkan grup: %w", err)
	}

	return fmt.Sprintf("✅ *Grup Berhasil Diizinkan!*\n\n📍 JID: `%s`\n👤 Diizinkan oleh: %s\n\nBot Lisa sekarang aktif dan siap melayani pesan di grup ini.", targetJID, sender), nil
}

func (h *Handler) handleRevokeGroup(ctx *router.MessageContext, payload string) (string, error) {
	var targetJID string

	fields := strings.Fields(payload)
	if len(fields) >= 3 && strings.Contains(fields[2], "@g.us") {
		targetJID = fields[2]
	} else if ctx.IsGroup {
		targetJID = ctx.ChatJID.String()
	} else {
		return "⚠️ Tentukan grup yang ingin dicabut:\n`@Bot cabut grup <groupID@g.us>` atau jalankan langsung di grup terkait.", nil
	}

	if err := h.db.RevokeGroup(ctx.Context, targetJID); err != nil {
		return "", fmt.Errorf("gagal mencabut grup: %w", err)
	}

	return fmt.Sprintf("🔒 *Izin Grup Dicabut!*\n\nJID: `%s`\nBot Lisa tidak akan lagi merespons di grup ini.", targetJID), nil
}

func (h *Handler) handleListGroups(ctx *router.MessageContext) (string, error) {
	groups, err := h.db.GetAllowedGroups(ctx.Context)
	if err != nil {
		return "", err
	}

	if len(groups) == 0 {
		return "📋 Belum ada grup yang diizinkan. Ketik `@Bot izinkan grup` di dalam grup yang diinginkan untuk membuka akses.", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📋 *Daftar Grup yang Diizinkan* (%d Grup):\n\n", len(groups)))
	for i, g := range groups {
		dateStr := g.CreatedAt.Format("02 Jan 2006")
		sb.WriteString(fmt.Sprintf("%d. *%s*\n   ID: `%s`\n   Ditambahkan: %s (%s)\n\n", i+1, g.GroupName, g.GroupJID, dateStr, g.AddedBy))
	}

	return strings.TrimSpace(sb.String()), nil
}

func (h *Handler) handleAddAdmin(ctx *router.MessageContext, payload string) (string, error) {
	parts := strings.Fields(payload)
	if len(parts) < 3 {
		return "⚠️ Format tidak lengkap. Gunakan contoh:\n`@Bot tambah admin 6281234567890`", nil
	}

	targetPhone := database.NormalizePhoneNumber(parts[2])
	if targetPhone == "" {
		return "⚠️ Nomor telepon admin tidak valid.", nil
	}

	sender := ctx.SenderName
	if sender == "" {
		sender = ctx.SenderJID.User
	}

	if err := h.db.AddAdmin(ctx.Context, targetPhone, "Admin", sender); err != nil {
		return "", fmt.Errorf("gagal menambah admin: %w", err)
	}

	return fmt.Sprintf("✅ *Admin Berhasil Ditambahkan!*\n\n📱 Nomor: +%s\n👤 Ditambahkan oleh: %s", targetPhone, sender), nil
}

func (h *Handler) handleRemoveAdmin(ctx *router.MessageContext, payload string) (string, error) {
	parts := strings.Fields(payload)
	if len(parts) < 3 {
		return "⚠️ Format tidak lengkap. Gunakan contoh:\n`@Bot hapus admin 6281234567890`", nil
	}

	targetPhone := database.NormalizePhoneNumber(parts[2])
	if targetPhone == "" {
		return "⚠️ Nomor telepon admin tidak valid.", nil
	}

	if err := h.db.RemoveAdmin(ctx.Context, targetPhone); err != nil {
		return "", fmt.Errorf("gagal menghapus admin: %w", err)
	}

	return fmt.Sprintf("✅ *Admin Berhasil Dihapus!*\n\n📱 Nomor: +%s", targetPhone), nil
}

func (h *Handler) handleListAdmins(ctx *router.MessageContext) (string, error) {
	admins, err := h.db.GetAdmins(ctx.Context)
	if err != nil {
		return "", err
	}

	if len(admins) == 0 {
		return "👤 Belum ada admin terdaftar.", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("👤 *Daftar Admin Bot Lisa* (%d Admin):\n\n", len(admins)))
	for i, a := range admins {
		dateStr := a.CreatedAt.Format("02 Jan 2006")
		sb.WriteString(fmt.Sprintf("%d. *+%s* (%s)\n   Ditambahkan: %s oleh %s\n\n", i+1, a.PhoneNumber, a.Name, dateStr, a.AddedBy))
	}

	return strings.TrimSpace(sb.String()), nil
}
