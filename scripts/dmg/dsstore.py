#!/usr/bin/env python3
"""Write the dmg root .DS_Store: window size/position, icon view without
toolbar/sidebar, background image alias and icon positions.

usage: dsstore.py OUT VOLNAME VOL_CREATED_UNIX BG_FOLDER_CNID BG_CNID APP_NAME

Needs: pip install ds_store mac_alias. Icon centers must match
scripts/dmg/background/main.go (600x380 window, app at 160,170,
Applications at 440,170).
"""
import datetime
import sys

from ds_store import DSStore
from mac_alias import (ALIAS_FIXED_DISK, ALIAS_KIND_FILE, Alias,
                       TargetInfo, VolumeInfo)

W, H = 600, 380
APP, LNK = (160, 170), (440, 170)


def main():
    out, vol, created, folder_cnid, bg_cnid, app = sys.argv[1:7]
    created = datetime.datetime.fromtimestamp(int(created), datetime.timezone.utc)
    alias = Alias(
        volume=VolumeInfo(vol, created, b"H+", ALIAS_FIXED_DISK, 0, b"\0\0",
                          posix_path="/Volumes/" + vol),
        target=TargetInfo(ALIAS_KIND_FILE, "background.png", int(folder_cnid), int(bg_cnid),
                          created, b"\0\0\0\0", b"\0\0\0\0",
                          folder_name=".background",
                          cnid_path=[int(folder_cnid)],
                          carbon_path=f"{vol}:.background:\0background.png",
                          posix_path="/.background/background.png"),
    )
    with DSStore.open(out, "w+") as d:
        d["."]["vSrn"] = ("long", 1)
        d["."]["bwsp"] = {
            "ShowStatusBar": False, "ShowToolbar": False, "ShowTabView": False,
            "ShowSidebar": False, "ShowPathbar": False, "ContainerShowSidebar": False,
            "WindowBounds": "{{200, 120}, {%d, %d}}" % (W, H + 22),
            "PreviewPaneVisibility": False, "SidebarWidth": 0,
        }
        d["."]["icvp"] = {
            "viewOptionsVersion": 1, "backgroundType": 2,
            "backgroundImageAlias": alias.to_bytes(),
            "backgroundColorRed": 1.0, "backgroundColorGreen": 1.0, "backgroundColorBlue": 1.0,
            "iconSize": 112.0, "textSize": 13.0, "gridSpacing": 100.0,
            "gridOffsetX": 0.0, "gridOffsetY": 0.0, "arrangeBy": "none",
            "labelOnBottom": True, "showIconPreview": True, "showItemInfo": False,
            "scrollPositionX": 0.0, "scrollPositionY": 0.0,
        }
        d["."]["icvl"] = ("type", b"icnv")
        d[app]["Iloc"] = APP
        d["Applications"]["Iloc"] = LNK


if __name__ == "__main__":
    main()
