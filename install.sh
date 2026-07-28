#!/usr/bin/env bash

# STYK VCS Installer Script
# Podporuje Linux, Android (Termux) i macOS

set -e

APP_NAME="styk"
GITHUB_REPO="tomas-trantina/styk" # Nahraďte vaším uživatelským jménem a repozitářem na GitHubu
RAW_BASE_URL="https://raw.githubusercontent.com/${GITHUB_REPO}/main"

# Barevný výstup
GREEN='\033[0;32m'
CYAN='\033[0;36m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BOLD='\033[1m'
NC='\033[0m'

echo -e "${CYAN}${BOLD}🚀 Instalace STYK VCS (Version Control System)...${NC}\n"

# 1. Detekce cílové složky pro binární soubor
if [ -n "$TERMUX_VERSION" ] || [ -d "/data/data/com.termux" ]; then
    BIN_DIR="${PREFIX}/bin"
elif [ -d "${HOME}/.local/bin" ]; then
    BIN_DIR="${HOME}/.local/bin"
elif [ -w "/usr/local/bin" ]; then
    BIN_DIR="/usr/local/bin"
else
    BIN_DIR="${HOME}/bin"
fi

mkdir -p "$BIN_DIR"

# 2. Získání nebo kompilace binárky
if [ -f "./styk" ]; then
    echo -e "${YELLOW}➔ Používám přítomný binární soubor ./styk...${NC}"
    cp "./styk" "${BIN_DIR}/${APP_NAME}"
elif command -v go >/dev/null 2>&1 && [ -f "main.go" ]; then
    echo -e "${YELLOW}➔ Kompiluji STYK VCS z Go zdrojových kódů...${NC}"
    go build -o "${BIN_DIR}/${APP_NAME}" .
else
    echo -e "${YELLOW}➔ Stahuji binární soubor styk z GitHubu...${NC}"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "${RAW_BASE_URL}/styk" -o "${BIN_DIR}/${APP_NAME}"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "${BIN_DIR}/${APP_NAME}" "${RAW_BASE_URL}/styk"
    else
        echo -e "${RED}Chyba: Není nainstalován curl ani wget.${NC}"
        exit 1
    fi
fi

chmod +x "${BIN_DIR}/${APP_NAME}"

# 3. Kontrola proměnné PATH
if [[ ":$PATH:" != *":$BIN_DIR:"* ]]; then
    echo -e "${YELLOW}⚠️ Upozornění: Složka ${BIN_DIR} není v proměnné PATH.${NC}"
    echo -e "Přidejte následující řádek do svého ~/.bashrc nebo ~/.zshrc:"
    echo -e "  ${CYAN}export PATH=\"\$PATH:${BIN_DIR}\"${NC}\n"
fi

echo -e "${GREEN}${BOLD}🎉 STYK VCS byl úspěšně nainstalován do ${BIN_DIR}/${APP_NAME}!${NC}"
echo -e "Aplikaci spustíte příkazem: ${CYAN}styk${NC}\n"
