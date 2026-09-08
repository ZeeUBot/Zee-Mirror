package basic

import (
	"sort"
	"strings"
	"zee-mirror/internal/service"
)

type HelpCommandEntry struct {
	Name        string
	Description string
	Category    string
	Emoji       string
	Aliases     []string
}

var registeredCommands []HelpCommandEntry

func SetRegisteredCommands(commands []HelpCommandEntry) {
	registeredCommands = commands
}

func GetHelpMirror() string {
	return service.HelpDetailMessage(
		"📥 MIRROR",
		"Upload file dari URL langsung ke Google Drive\\.\nFile akan disimpan di cloud storage tanpa menyimpan di server\\.",
		"• `/mirror` atau `/m` <URL\\> ─ Download dan upload ke Drive\n• Reply ke file dengan `/mirror`\n\n⚙️ *FLAGS OPSIONAL*\n• `\\-n <nama\\>` ─ Rename file\n• `\\-z` ─ Zip file sebelum upload\n• `\\-uz` ─ Unzip setelah download\n• `\\-p <pass\\>` ─ Password zip/unzip",
		"`/mirror https://example\\.com/file\\.zip`\n`/mirror \\-n \"My File\\.zip\" https://example\\.com/file\\.zip`\n`/mirror \\-z https://example\\.com/folder`\n`/mirror \\-uz \\-p secret https://file\\.rar`",
		"",
	)
}

func GetHelpLeech() string {
	return service.HelpDetailMessage(
		"📤 LEECH",
		"Download file dari URL ke server bot\\.\nFile akan dikirim langsung ke chat Telegram setelah selesai\\.",
		"• `/leech` atau `/l` <URL\\> ─ Download ke server\n• Reply ke file dengan `/leech`\n\n⚙️ *FLAGS OPSIONAL*\n• `\\-n <nama\\>` ─ Rename file\n• `\\-z` ─ Zip file sebelum kirim\n• `\\-uz` ─ Unzip setelah download\n• `\\-p <pass\\>` ─ Password zip/unzip",
		"`/leech https://example\\.com/video\\.mp4`\n`/leech \\-n \"My Video\\.mp4\" https://example\\.com/video\\.mp4`\n`/leech \\-uz https://archive\\.rar`",
		"",
	)
}

func GetHelpYTDLP() string {
	return service.HelpDetailMessage(
		"🎬 YT-DLP MIRROR",
		"Download video/audio dari 1000\\+ situs langsung ke Drive\\.\nSupport: YouTube, TikTok, Twitter/X, Instagram, Facebook, Vimeo, dll\\.",
		"• `/ytdlp` atau `/y` <URL\\> ─ Download dengan pilihan kualitas\n\n⚙️ *FLAGS OPSIONAL*\n• `\\-q <quality\\>` ─ Pilih kualitas \\(best, 1080p, 720p\\)\n• `\\-n <nama\\>` ─ Rename file\n• `\\-z` ─ Zip file\n• `\\-p <pass\\>` ─ Password zip\n\n✨ *FITUR*\n• Pilih kualitas video \\(360p\\-8K\\)\n• Download audio only \\(MP3\\)\n• Subtitle otomatis",
		"`/ytdlp https://youtube\\.com/watch?v=xxxxx`\n`/ytdlp \\-q 1080p https://tiktok\\.com/@user/video/123`",
		"",
	)
}

func GetHelpYTDLPLeech() string {
	return service.HelpDetailMessage(
		"🎬 YT-DLP LEECH",
		"Download video/audio dari 1000\\+ situs langsung ke Telegram\\.\nSupport: YouTube, TikTok, Twitter/X, Instagram, Facebook, Vimeo, dll\\.",
		"• `/ytdlpleech` atau `/yl` <URL\\> ─ Download ke Telegram\n\n⚙️ *FLAGS OPSIONAL*\n• `\\-q <quality\\>` ─ Pilih kualitas \\(best, 1080p, 720p\\)\n• `\\-n <nama\\>` ─ Rename file\n• `\\-z` ─ Zip file\n• `\\-p <pass\\>` ─ Password zip\n\n✨ *FITUR*\n• Pilih kualitas video \\(360p\\-8K\\)\n• Kirim sebagai video Telegram",
		"`/ytdlpleech https://youtube\\.com/watch?v=xxxxx`",
		"",
	)
}

func GetHelpViking() string {
	return service.HelpDetailMessage(
		"⚔️ VIKING FILE",
		"Download dari URL dan upload ke ViKiNG FiLE\\.\nSupport anonymous upload atau user upload jika hash di-set\\.",
		"• `/viking` atau `/v` <URL\\> ─ Upload ke Viking\n\n⚙️ *FLAGS OPSIONAL*\n• `\\-n <nama\\>` ─ Rename file\n• `\\-z` ─ Zip file\n• `\\-p <pass\\>` ─ Password zip",
		"`/viking https://example\\.com/file\\.zip`",
		"",
	)
}

func GetHelpTorrent() string {
	return service.HelpDetailMessage(
		"🧲 TORRENT",
		"Download file via magnet link atau file torrent \\(\\.torrent\\)\\.\nBot akan otomatis mendeteksi jika Anda mengirim magnet link tanpa command\\.",
		"• `/torrent` atau `/t` <magnet\\> ─ Download dari magnet\n• Reply ke \\.torrent dengan `/torrent`\n• Kirim magnet link langsung \\(Auto\\-Detect\\)\n\n⚙️ *FLAGS OPSIONAL*\n• `\\-n <nama\\>` ─ Rename file/folder\n• `\\-z` ─ Zip file sebelum upload\n• `\\-uz` ─ Unzip setelah download\n• `\\-p <pass\\>` ─ Password zip/unzip\n\n✨ *FITUR*\n• Support magnet link dan file \\.torrent\n• Multi\\-connection download\n• Resume download",
		"`/torrent magnet:?xt=urn:btih:xxxxx`\n`/torrent \\-z magnet:?xt=urn:btih:xxxxx`",
		"💡 *AUTO\\-DETECT:* Anda bisa langsung mengirim magnet link, bot akan otomatis memprosesnya\\.",
	)
}

func GetHelpClone() string {
	return service.HelpDetailMessage(
		"📋 CLONE",
		"Clone/salin file atau folder dari Google Drive ke Drive tujuan\\.",
		"• `/clone` atau `/cl` <drive\\_url\\> ─ Clone ke storage aktif\n\n🔗 *URL YANG DIDUKUNG*\n• `drive\\.google\\.com/file/d/xxx`\n• `drive\\.google\\.com/drive/folders/xxx`\n• `drive\\.google\\.com/open?id=xxx`",
		"`/clone https://drive\\.google\\.com/file/d/abc123`",
		"",
	)
}

func GetHelpBatch() string {
	return service.HelpDetailMessage(
		"📦 BATCH",
		"Download multiple URLs sekaligus dalam satu batch\\.",
		"/batch\nURL1\nURL2\nURL3\n\n⚙️ *FLAGS OPSIONAL* \\(First Line\\)\n• `\\-name <name\\>` ─ Set nama batch\n• `\\-z` / `\\-zip` ─ Zip semua hasil\n• `\\-p <pass\\>` ─ Password zip\n• `\\-priority <1\\-10\\>` ─ Set prioritas\n\n✨ *FITUR*\n• Download parallel\n• Progress tracking per URL\n• Auto retry jika gagal",
		"/batch\nhttps://file1\\.com/a\\.zip\nhttps://file2\\.com/b\\.zip\n\n`/batch \\-name Liburan \\-z`\n`https://photo\\.com/a\\.jpg`\n`https://photo\\.com/b\\.jpg`",
		"",
	)
}

func GetHelpSearch() string {
	return service.HelpDetailMessage(
		"🔍 SEARCH",
		"Cari torrent dari berbagai sumber/tracker\\.",
		"• `/search <keyword\\>` ─ Cari torrent\n\n✨ *FITUR*\n• Multi\\-tracker search\n• Filter hasil\n• Download langsung dari hasil",
		"`/search ubuntu 22\\.04`\n`/search archlinux iso`",
		"",
	)
}

func GetHelpStatus() string {
	return service.HelpDetailMessage(
		"📊 STATUS",
		"Melihat semua task yang sedang aktif \\(download/upload\\)\\.",
		"• `/status` atau `/st` ─ Tampilkan semua task aktif\n\n📋 *INFO DITAMPILKAN*\n• Nama file\n• Progress \\(%\\)\n• Kecepatan download/upload\n• ETA \\(estimasi waktu selesai\\)\n• Status \\(downloading/uploading\\)\n\n🔘 *TOMBOL AKSI*\n• Refresh ─ Update status\n• Cancel ─ Batalkan task",
		"`/status`",
		"",
	)
}

func GetHelpStats() string {
	return service.HelpDetailMessage(
		"📈 STATS",
		"Melihat statistik penggunaan bot\\.",
		"• `/stats` ─ Tampilkan statistik\n\n📋 *INFO DITAMPILKAN*\n• Total download\n• Total upload\n• Data yang diproses\n• Uptime bot\n• Task completed/failed",
		"`/stats`",
		"",
	)
}

func GetHelpSystem() string {
	return service.HelpDetailMessage(
		"🖥️ SYSTEM",
		"Melihat informasi lengkap sistem server\\.",
		"• `/system` ─ Tampilkan info sistem\n\n📋 *INFO DITAMPILKAN*\n• CPU usage\n• RAM usage\n• Disk space\n• Network stats\n• Uptime server\n• OS info",
		"`/system`",
		"",
	)
}

func GetHelpHealth() string {
	return service.HelpDetailMessage(
		"🏥 HEALTH",
		"Mengecek kesehatan semua komponen bot\\.",
		"• `/health` ─ Cek kesehatan sistem\n\n🔍 *KOMPONEN YANG DICEK*\n• Aria2 daemon\n• Rclone\n• Database\n• Disk space\n• Memory\n• Network connectivity",
		"`/health`",
		"",
	)
}

func GetHelpLogs() string {
	return service.HelpDetailMessage(
		"📜 LOGS",
		"Melihat log aktivitas bot\\.",
		"• `/logs` ─ Lihat 50 baris terakhir\n• `/logs <n\\>` ─ Lihat n baris terakhir",
		"`/logs` ─ Default 50 baris\n`/logs 100` ─ 100 baris terakhir\n`/logs 20` ─ 20 baris terakhir",
		"",
	)
}

func GetHelpPing() string {
	return service.HelpDetailMessage(
		"🏓 PING",
		"Mengecek latency/response time bot\\.",
		"• `/ping` ─ Cek latency\n\n📋 *INFO DITAMPILKAN*\n• Response time \\(ms\\)\n• Bot status",
		"`/ping`",
		"",
	)
}

func GetHelpSpeed() string {
	return service.HelpDetailMessage(
		"🚀 SPEED",
		"Test kecepatan internet server\\.",
		"• `/speed` ─ Jalankan speedtest\n\n📋 *INFO DITAMPILKAN*\n• Download speed\n• Upload speed\n• Ping\n• Server lokasi",
		"`/speed`",
		"",
	)
}

func GetHelpLs() string {
	return service.HelpDetailMessage(
		"📂 LIST (LS / DIR)",
		"Melihat isi folder di cloud storage\\.\nPerintah ini mendukung navigasi interaktif melalui tombol\\.",
		"• `/ls` ─ Lihat root folder\n• `/dir` ─ Alias dari /ls\n• `/ls <path\\>` ─ Lihat folder tertentu",
		"`/ls` ─ Root folder\n`/ls Movies` ─ Folder Movies\n`/ls Movies/2024` ─ Subfolder",
		"💡 *TIP:* Anda bisa klik nama folder di tombol untuk masuk ke dalam folder tersebut\\.",
	)
}

func GetHelpMkdir() string {
	return service.HelpDetailMessage(
		"📁 MKDIR",
		"Membuat folder baru di cloud storage\\.",
		"• `/mkdir <nama\\_folder\\>` ─ Buat folder",
		"`/mkdir Movies`\n`/mkdir Backup/2024`",
		"",
	)
}

func GetHelpRm() string {
	return service.HelpDetailMessage(
		"🗑️ REMOVE (RM)",
		"Menghapus file atau folder di cloud storage\\.",
		"• `/rm <nama\\>` ─ Hapus file/folder\n\n⚠️ *PERINGATAN*\nPenghapusan bersifat permanen\\!",
		"`/rm old_file\\.zip`\n`/rm Temp/cache`",
		"",
	)
}

func GetHelpMv() string {
	return service.HelpDetailMessage(
		"📦 MOVE (MV)",
		"Memindahkan file/folder ke lokasi lain atau ganti nama\\.",
		"• `/mv <source\\> <destination\\>`",
		"`/mv file\\.zip Backup/`\n`/mv old\\.mp4 new\\.mp4`",
		"",
	)
}

func GetHelpShare() string {
	return service.HelpDetailMessage(
		"🔗 SHARE",
		"Membuat link berbagi untuk file/folder\\.",
		"• `/share <nama\\>` ─ Generate share link",
		"`/share movie\\.mp4`\n`/share SharedFolder`",
		"",
	)
}

func GetHelpFind() string {
	return service.HelpDetailMessage(
		"🔍 FIND",
		"Mencari file di cloud storage secara rekursif\\.",
		"• `/find <keyword\\>` ─ Cari file",
		"`/find movie`\n`/find \\.mp4` ─ Cari semua MP4",
		"",
	)
}

func GetHelpExtractAudio() string {
	return service.HelpDetailMessage(
		"🎵 EXTRACT AUDIO",
		"Mengekstrak audio dari file video\\.",
		"• Reply ke video dengan `/extractaudio`\n\n📋 *FORMAT OUTPUT*\nMP3 \\(LAME Codec\\)",
		"Reply ke video lalu ketik `/extractaudio`",
		"",
	)
}

func GetHelpCompress() string {
	return service.HelpDetailMessage(
		"🗜️ COMPRESS",
		"Mengkompresi ukuran file video dengan FFmpeg\\.",
		"• Reply ke video dengan `/compress [low|medium|high]`\n\n✨ *FITUR*\n• Kurangi bitrate video\n• Auto scale audio",
		"`/compress` \\(Default medium\\)\n`/compress high` \\(Kualitas lebih baik\\)",
		"",
	)
}

func GetHelpThumbnail() string {
	return service.HelpDetailMessage(
		"🖼️ THUMBNAIL",
		"Generate thumbnail dari video pada detik tertentu\\.",
		"• Reply ke video dengan `/thumbnail [HH:MM:SS]`\n\n✨ *FITUR*\n• Default: Detik ke-5",
		"`/thumbnail` ─ Detik ke-5\n`/thumbnail 00:01:30` ─ Menit 1\\.5",
		"",
	)
}

func GetHelpScreenshots() string {
	return service.HelpDetailMessage(
		"📸 SCREENSHOTS",
		"Membuat multiple screenshot dari video secara otomatis\\.",
		"• Reply ke video dengan `/screenshots [jumlah]`\n\n✨ *FITUR*\n• Antara 1 s/d 10 screenshot",
		"`/screenshots` ─ Default 4 SS\n`/screenshots 10` ─ Maksimum 10 SS",
		"",
	)
}

func GetHelpSubtitle() string {
	return service.HelpDetailMessage(
		"💬 SOFT-SUB (SUBTITLE)",
		"Embed subtitle track ke dalam container video \\(Soft-sub\\)\\.\nSubtitle bisa di-on/off saat nonton.",
		"1\\. Kirim subtitle \\(\\.srt/\\.vtt\\)\n2\\. Reply ke video dengan `/subtitle`",
		"Reply ke video lalu ketik `/subtitle`",
		"",
	)
}

func GetHelpHardsub() string {
	return service.HelpDetailMessage(
		"🔥 HARD-SUB (BURN SUBTITLE)",
		"Membakar subtitle permanen ke dalam gambar video \\(Hard-sub\\)\\.\nSubtitle akan menempel selamanya di video dan tidak bisa dimatikan.",
		"1\\. Kirim subtitle \\(\\.srt/\\.ssa/\\.ass\\)\n2\\. Reply ke video dengan `/hardsub`",
		"Reply ke video lalu ketik `/hardsub`",
		"⚠️ *INFO:* Proses ini memakan waktu lama karena bot harus melakukan re-encoding video.",
	)
}

func GetHelpRescale() string {
	return service.HelpDetailMessage(
		"📐 RESCALE (TRANSCODE)",
		"Mengubah resolusi/dimensi video \\(Transcoding\\)\\.",
		"• Reply ke video dengan `/rescale <preset|WxH\\>`\n\n🏷️ *PRESET*\n`4k`, `2k`, `1080p`, `720p`, `480p`, `360p`",
		"Reply ke video dengan `/rescale 720p` atau `/rescale 1280x720`",
		"",
	)
}

func GetHelpConvert() string {
	return service.HelpDetailMessage(
		"🔄 CONVERT",
		"Konversi format file video/audio tanpa mengganti kualitas \\(Remux\\)\\.",
		"• Reply ke file dengan `/convert <format\\>`\n\n📋 *SUPPORT*\nmp4, mkv, avi, mov, webm, mp3, aac, flac, wav",
		"`/convert mp4`\n`/convert mp3`",
		"",
	)
}

func GetHelpMediaInfo() string {
	return service.HelpDetailMessage(
		"ℹ️ MEDIA INFO",
		"Melihat informasi detail teknis file media menggunakan ffprobe\\.",
		"• Reply ke file dengan `/mediainfo`\n\n📋 *INFO*\nCodec, Resolusi, Bitrate, Duration, Streams",
		"Reply ke video lalu ketik `/mediainfo`",
		"",
	)
}

func GetHelpCancel() string {
	return service.HelpDetailMessage(
		"❌ CANCEL",
		"Membatalkan task tertentu\\.",
		"• `/cancel` atau `/c` <task\\_id\\> ─ Batalkan task\n• Klik tombol Cancel di status",
		"`/cancel abc123`",
		"💡 *TIP:* Lihat task ID dengan `/status`",
	)
}

func GetHelpCancelAll() string {
	return service.HelpDetailMessage(
		"🚫 CANCEL ALL",
		"Membatalkan SEMUA task yang sedang berjalan\\.",
		"• `/cancelall` ─ Batalkan semua task",
		"`/cancelall`",
		"⚠️ Semua download/upload aktif akan dihentikan\\!",
	)
}

func GetHelpStorages() string {
	return service.HelpDetailMessage(
		"📋 STORAGES",
		"Melihat daftar cloud storage yang tersedia\\.",
		"• `/storages` ─ Lihat semua storage",
		"`/storages`",
		"",
	)
}

func GetHelpSetStorage() string {
	return service.HelpDetailMessage(
		"⚙️ SET STORAGE",
		"Mengatur storage aktif untuk proses Mirror/Clone\\.",
		"• `/setstorage <nama\\>` ─ Set storage",
		"`/setstorage gdrive`",
		"",
	)
}

func GetHelpAuthorize() string {
	return service.HelpDetailMessage(
		"✅ AUTHORIZE",
		"Menambahkan user baru yang diizinkan menggunakan bot\\.",
		"• `/authorize <user\\_id\\> [nama]` ─ Tambah user\n• Reply ke user dengan `/authorize`",
		"`/authorize 123456789 Zee`",
		"",
	)
}

func GetHelpUnauthorize() string {
	return service.HelpDetailMessage(
		"❌ UNAUTHORIZE",
		"Menghapus akses user dari bot\\.",
		"• `/unauthorize <user\\_id\\>` ─ Hapus akses",
		"`/unauthorize 123456789`",
		"",
	)
}

func GetHelpUsers() string {
	return service.HelpDetailMessage(
		"👥 USERS",
		"Melihat daftar semua user yang terdaftar di database\\.",
		"• `/users` ─ Lihat semua user",
		"`/users`",
		"",
	)
}

func GetHelpSetAlertChannel() string {
	return service.HelpDetailMessage(
		"🚨 SET ALERT CHANNEL",
		"Mengatur channel untuk notifikasi log error/penting\\.",
		"• `/setalertchannel <channel\\_id\\>`",
		"`/setalertchannel \\-1001234567890`",
		"",
	)
}

func GetHelpSetLogChannel() string {
	return service.HelpDetailMessage(
		"📜 SET LOG CHANNEL",
		"Mengatur channel khusus untuk dump log aktivitas\\.",
		"• `/setlogchannel <channel\\_id\\>`",
		"`/setlogchannel -1001234567890`",
		"💡 *CATATAN:* ID channel biasanya dimulai dengan \\-100\\.",
	)
}

func GetHelpRecover() string {
	return service.HelpDetailMessage(
		"🔄 RECOVER",
		"Memulihkan task yang terhenti akibat restart bot\\.",
		"• `/recover` ─ Pulihkan task",
		"`/recover`",
		"",
	)
}

func GetHelpRecoveryStatus() string {
	return service.HelpDetailMessage(
		"📊 RECOVERY STATUS",
		"Melihat statistik task yang bisa dipulihkan\\.",
		"• `/recoverystatus` ─ Lihat status",
		"`/recoverystatus`",
		"",
	)
}

func GetHelpSettings() string {
	return service.HelpDetailMessage(
		"⚙️ SETTINGS",
		"Mengatur preferensi dan konfigurasi bot\\.",
		"• `/settings` ─ Buka menu pengaturan\n\n📋 *OPSI*\n• Auto Delete\n• Default Mode",
		"`/settings`",
		"",
	)
}

func getHelpAllCommands() string {
	return GenerateAllCommandsHelp(registeredCommands)
}

func escapeMarkdownV2(text string) string {
	specialChars := []string{"_", "*", "[", "]", "(", ")", "~", "`", ">", "#", "+", "-", "=", "|", "{", "}", ".", "!"}
	result := text
	for _, ch := range specialChars {
		result = strings.ReplaceAll(result, ch, "\\"+ch)
	}
	return result
}

func GenerateAllCommandsHelp(commands []HelpCommandEntry) string {
	if len(commands) == 0 {
		return service.ProfessionalMessage("ALL COMMANDS", "📋 *DAFTAR SELURUH PERINTAH BOT*\n\nTidak ada command yang terdaftar\\.")
	}

	categoryOrder := []string{"download", "monitor", "files", "media", "task", "storage", "admin", "recovery", "general"}
	categoryNames := map[string]string{
		"download": "📥 DOWNLOAD",
		"monitor":  "📊 MONITOR & SYSTEM",
		"files":    "📁 STORAGE & FILES",
		"media":    "🎞️ MEDIA TOOLS",
		"task":     "📋 TASK MANAGEMENT",
		"storage":  "💾 STORAGE",
		"admin":    "👑 ADMINISTRATION",
		"recovery": "🔧 RECOVERY",
		"general":  "🛠️ GENERAL",
	}

	byCategory := make(map[string][]HelpCommandEntry)
	for _, cmd := range commands {
		byCategory[cmd.Category] = append(byCategory[cmd.Category], cmd)
	}

	for cat := range byCategory {
		sort.Slice(byCategory[cat], func(i, j int) bool {
			return byCategory[cat][i].Name < byCategory[cat][j].Name
		})
	}

	var sb strings.Builder
	sb.WriteString("📋 *DAFTAR SELURUH PERINTAH BOT*\n")

	for _, cat := range categoryOrder {
		cmds, ok := byCategory[cat]
		if !ok || len(cmds) == 0 {
			continue
		}
		sb.WriteString("\n")
		sb.WriteString(categoryNames[cat])
		sb.WriteString("\n")
		for _, cmd := range cmds {
			sb.WriteString("• `/")
			sb.WriteString(cmd.Name)
			sb.WriteString("`")
			if len(cmd.Aliases) > 0 {
				sb.WriteString(" \\(")
				for i, alias := range cmd.Aliases {
					if i > 0 {
						sb.WriteString(", ")
					}
					sb.WriteString("/")
					sb.WriteString(alias)
				}
				sb.WriteString("\\)")
			}
			sb.WriteString(" ─ ")
			sb.WriteString(escapeMarkdownV2(cmd.Description))
			sb.WriteString("\n")
		}
	}

	return service.ProfessionalMessage("ALL COMMANDS", sb.String())
}
