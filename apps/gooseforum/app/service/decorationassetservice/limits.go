package decorationassetservice

// 管理员上传的头像框/徽章图（SiteManager 专用）允许动图，因此比帖子图片的 4MB 上限宽一档：
// 8MB 字节 + 48M 合成像素（≈192MB 解码内存上限），同时保留画布 ≤4096 与帧数 ≤300 的硬限制。
// 帖子图片、头像等其它上传路径仍走 filedata.MaxFileSize(4MB)。
const (
	MaxArtworkBytes          = 8 << 20
	animatedArtworkMaxFrames = 300
	animatedArtworkMaxPixels = 48 << 20
)
