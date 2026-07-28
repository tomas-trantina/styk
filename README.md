<div align="center">

# 🚀 STYK VCS

### *Moderní, bleskově rychlý a lehký verzovací & synchronizační systém přes SSH (Go + TUI)*

---

[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8.svg?style=flat&logo=go)](https://go.dev)
[![Version](https://img.shields.io/badge/Version-v3.0.0-orange.svg)]()
[![Platform](https://img.shields.io/badge/Platform-Linux%20%7C%20Android%20Termux%20%7C%20macOS-brightgreen.svg)]()
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

</div>

---

## 📝 Popis projektu (Description)

> **STYK VCS je extrémně rychlý, lehký a bezpečný verzovací systém a synchronizační nástroj navržený v jazyce Go pro bezproblémovou správu a zrcadlení projektů mezi počítačem, vzdáleným serverem a mobilním telefonem (Android Termux) přes SSH. Využívá zstd kompresi pro úsporu dat a nabízí jak přehledné CLI, tak i moderní interaktivní TUI rozhraní v terminálu.**

---

## ✨ Hlavní Vlastnosti

- ⚡ **Vysoký výkon & ZSTD komprese** – Inkrementální diffy i snapshoty jsou komprimovány pomocí algoritmu `zstd` pro bleskový přenos po síti.
- 📱 **Plná podpora pro Android Termux** – Navrženo tak, aby fungovalo bezchybně na PC i přímo v prostředí Android Termux na mobilních zařízeních.
- 🔒 **Bezpečné SSH spojení** – Integrovaná podpora SSH agenta i klíčů pro bezpečné šifrované spojení bez zápisu hesel do paměti.
- 🖥️ **Interaktivní TUI (Terminal UI)** – Vestavěné terminálové rozhraní postavené na rozhraní `Charm Bubbletea / Lipgloss` pro pohodlnou prohlídku historie, diffů a spravovaných projektů.
- ⚙️ **Multi-Profile konfigurace** – Snadná správa více serverů, uživatelů a vzdálených cílů.
- 🛡️ **Integrita a diagnostika** – Příkazy `styk doctor` a `styk verify` pro kontrolu stavu prostředí a datové integrity.

---

## ⚡ Jednořádková Instalace

Nainstalujte STYK VCS na svůj systém (Linux, Android Termux, macOS) pomocí jediného příkazu v terminálu:

```bash
curl -fsSL https://raw.githubusercontent.com/tomas-trantina/styk/main/install.sh | bash
```
---

## 💻 Přehled Příkazů (CLI Reference)

STYK VCS nabízí intuitivní sadu příkazů pro veškeré operace:

| Příkaz | Zkráceně | Popis |
| :--- | :--- | :--- |
| `styk new <název>` | | Inicializuje nový projekt na vzdáleném serveru |
| `styk init <název>` | | Inicializuje aktuální lokální adresář |
| `styk add <zpráva>` | | Uloží novou verzi (diff + zstd komprese) |
| `styk snapshot <zpráva>` | | Uloží kompletní snapshot projektu |
| `styk clone <název>` | | Klonuje projekt ze vzdáleného serveru |
| `styk checkout <v>` | `styk co` | Přepne projekt na konkrétní verzi |
| `styk back` | | Vrátí se k předchozí verzi |
| `styk diff [v1] [v2]` | | Zobrazí rozdíly mezi verzemi s barevným odlišením |
| `styk status` | `styk st` | Zobrazí změny v aktuálním adresáři |
| `styk log` | | Vypíše historii verzí projektu |
| `styk tui` | | Spustí interaktivní terminálové rozhraní |
| `styk config` | | Interaktivní nastavení SSH profilů |
| `styk doctor` | | Provede kompletní diagnostiku prostředí a SSH |
| `styk verify` | | Ověří datovou integritu projektu |
| `styk list` | `styk ls` | Vypíše přehled dostupných projektů na serveru |

---

## 🛠️ Manuální Instalace & Kompilace

Pokud si přejete skompilovat aplikaci ze zdrojových kódů:

```bash
# Klonování repozitáře
git clone https://github.com/USERNAME/styk.git
cd styk

# Spuštění instalačního skriptu (kompiluje přes Go nebo nakopíruje přítomnou binárku)
chmod +x install.sh
./install.sh
```

Nebo přímo pomocí nástroje `go`:

```bash
go build -o styk .
```

---

## 🗑️ Odinstalace

Pro kompletní odstranění spouštěče z vašeho systému použijte odinstalační skript:

```bash
curl -fsSL https://raw.githubusercontent.com/USERNAME/styk/main/uninstall.sh | bash
```

Nebo lokálně:

```bash
./uninstall.sh
```

---

## 📄 Licence

Tento projekt je vydán pod licencí [MIT](LICENSE).
