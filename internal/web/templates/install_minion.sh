#!/bin/bash

# Script d'installation de Minion
# Usage: curl {{.BaseURL}}/install_minion.sh | sh

set -e

# Couleurs pour l'affichage
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
BASE_URL="{{.BaseURL}}"
BINARY_NAME="minion"

# Fonction pour afficher les messages
log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Fonction pour détecter l'OS
detect_os() {
    case "$(uname -s)" in
        Linux*)     OS="linux" ;;
        Darwin*)    OS="darwin" ;;
        CYGWIN*|MINGW*|MSYS*) OS="windows" ;;
        *)          OS="unknown" ;;
    esac
    
    if [ "$OS" = "unknown" ]; then
        log_error "OS non supporté: $(uname -s)"
        exit 1
    fi
    
    log_info "OS détecté: $OS"
}

# Fonction pour détecter l'architecture
detect_arch() {
    ARCH=$(uname -m)
    
    case "$ARCH" in
        x86_64|amd64)   ARCH="amd64" ;;
        aarch64|arm64)  ARCH="arm64" ;;
        armv7l)         ARCH="arm" ;;
        i386|i686)      ARCH="386" ;;
        *)              
            log_error "Architecture non supportée: $ARCH"
            exit 1
            ;;
    esac
    
    log_info "Architecture détectée: $ARCH"
}

# Fonction pour construire le nom du binaire
build_binary_name() {
    local suffix=""
    
    # Déterminer le suffixe selon la plateforme
    case "$OS" in
        linux)
            if [ "$ARCH" = "amd64" ]; then
                suffix="-linux"
            fi
            ;;
        windows)
            suffix=".exe"
            if [ "$ARCH" = "amd64" ]; then
                suffix="-windows.exe"
            fi
            ;;
        darwin)
            if [ "$ARCH" = "amd64" ]; then
                suffix="-darwin"
            fi
            ;;
    esac
    
    BINARY_FILE="${BINARY_NAME}${suffix}"
    DOWNLOAD_URL="${BASE_URL}/download/minion/${OS}-${ARCH}${suffix}"
    
    log_info "Binaire cible: $BINARY_FILE"
    log_info "URL de téléchargement: $DOWNLOAD_URL"
}

# Fonction pour télécharger le binaire
download_binary() {
    local temp_file="/tmp/${BINARY_FILE}"
    
    log_info "Téléchargement du binaire..."
    
    # Utiliser curl ou wget selon ce qui est disponible
    if command -v curl >/dev/null 2>&1; then
        if ! curl -L -o "$temp_file" "$DOWNLOAD_URL"; then
            log_error "Échec du téléchargement avec curl"
            exit 1
        fi
    elif command -v wget >/dev/null 2>&1; then
        if ! wget -O "$temp_file" "$DOWNLOAD_URL"; then
            log_error "Échec du téléchargement avec wget"
            exit 1
        fi
    else
        log_error "curl ou wget requis pour le téléchargement"
        exit 1
    fi
    
    # Vérifier que le fichier a été téléchargé
    if [ ! -f "$temp_file" ]; then
        log_error "Le fichier n'a pas été téléchargé"
        exit 1
    fi
    
    # Vérifier la taille du fichier
    if [ ! -s "$temp_file" ]; then
        log_error "Le fichier téléchargé est vide"
        rm -f "$temp_file"
        exit 1
    fi
    
    log_info "Téléchargement terminé avec succès"
    
    # Déplacer le binaire vers le répertoire de destination
    local install_dir="/usr/local/bin"
    local final_path="${install_dir}/${BINARY_NAME}"
    
    # Créer le répertoire s'il n'existe pas
    if [ ! -d "$install_dir" ]; then
        log_warn "Création du répertoire $install_dir"
        sudo mkdir -p "$install_dir"
    fi
    
    # Copier et rendre exécutable
    log_info "Installation du binaire vers $final_path"
    sudo cp "$temp_file" "$final_path"
    sudo chmod +x "$final_path"
    
    # Nettoyer le fichier temporaire
    rm -f "$temp_file"
    
    log_info "Installation terminée avec succès"
}

# Fonction pour vérifier l'installation
verify_installation() {
    if command -v "$BINARY_NAME" >/dev/null 2>&1; then
        log_info "Vérification de l'installation..."
        local version_output
        if version_output=$("$BINARY_NAME" --version 2>/dev/null); then
            log_info "Version installée: $version_output"
        else
            log_warn "Impossible d'obtenir la version, mais le binaire est installé"
        fi
        return 0
    else
        log_error "Le binaire n'est pas accessible dans le PATH"
        return 1
    fi
}

# Fonction pour exécuter le binaire
run_minion() {
    log_info "Démarrage de Minion..."
    
    # Vérifier si des paramètres ont été passés via des variables d'environnement
    local minion_args=""
    
    if [ -n "$NEXUS_SERVER" ]; then
        minion_args="$minion_args --server $NEXUS_SERVER"
    fi
    
    if [ -n "$MINION_NAME" ]; then
        minion_args="$minion_args --name $MINION_NAME"
    fi
    
    if [ -n "$MINION_TAGS" ]; then
        minion_args="$minion_args --tags $MINION_TAGS"
    fi
    
    if [ -z "$minion_args" ]; then
        log_info "Aucune configuration fournie. Utilisation des paramètres par défaut."
        log_info "Variables d'environnement disponibles:"
        log_info "  NEXUS_SERVER - Adresse du serveur Nexus"
        log_info "  MINION_NAME  - Nom du minion"
        log_info "  MINION_TAGS  - Tags du minion"
        log_info ""
        log_info "Exemple:"
        log_info "  NEXUS_SERVER=nexus.example.com:8080 MINION_NAME=web-server-01 $0"
    fi
    
    # Exécuter le minion
    log_info "Commande exécutée: $BINARY_NAME $minion_args"
    exec "$BINARY_NAME" $minion_args
}

# Fonction principale
main() {
    log_info "=== Installation de Minion ==="
    
    # Vérifier les prérequis
    if [ "$EUID" -eq 0 ]; then
        log_warn "Attention: exécution en tant que root"
    fi
    
    # Détecter l'environnement
    detect_os
    detect_arch
    
    # Construire les informations de téléchargement
    build_binary_name
    
    # Télécharger et installer
    download_binary
    
    # Vérifier l'installation
    if verify_installation; then
        log_info "=== Installation réussie ==="
        
        # Exécuter le minion
        run_minion
    else
        log_error "=== Échec de l'installation ==="
        exit 1
    fi
}

# Exécuter le script principal
main "$@"