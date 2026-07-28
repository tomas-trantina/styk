#!/usr/bin/env bash

# STYK VCS Uninstaller Script

set -e

APP_NAME="styk"

# Barevný výstup
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BOLD='\033[1m'
NC='\033[0m'

echo -e "${YELLOW}${BOLD}🗑️ Odinstalace STYK VCS (${APP_NAME})...${NC}\n"

POSSIBLE_BINS=(
    "${PREFIX}/bin/${APP_NAME}"
    "${HOME}/.local/bin/${APP_NAME}"
    "/usr/local/bin/${APP_NAME}"
    "${HOME}/bin/${APP_NAME}"
)

REMOVED=0
for bin_path in "${POSSIBLE_BINS[@]}"; do
    if [ -f "$bin_path" ]; then
        rm -f "$bin_path"
        echo -e "${GREEN}✔ Odstraněna binárka ${bin_path}${NC}"
        REMOVED=1
    fi
done

if [ $REMOVED -eq 0 ]; then
    echo -e "${RED}Spouštěč ${APP_NAME} nebyl nalezen v obvyklých umístěních.${NC}"
fi

echo -e "\n${GREEN}${BOLD}✨ STYK VCS byl úspěšně odinstalován.${NC}\n"
