//go:build !windows

package steamgamedl

const SteamMetadataLink = SteamMetadataServerLink + "steam_cmd_linux"
const DownloadOs = "linux"
const AssetName = "DepotDownloader-linux-${arch}.zip"
const DepotDownloaderBinary = "DepotDownloader"
