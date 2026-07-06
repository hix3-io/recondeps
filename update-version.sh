#!/bin/bash

# Script de mise à jour de version pour ReconDeps
# Usage: ./update-version.sh <new_version>
# Exemple: ./update-version.sh 0.2.0

set -e

if [ $# -eq 0 ]; then
    echo "❌ Usage: $0 <new_version>"
    echo "📋 Exemples:"
    echo "   $0 0.1.1  # Patch release"
    echo "   $0 0.2.0  # Minor release" 
    echo "   $0 1.0.0  # Major release"
    echo ""
    echo "📖 Version actuelle:"
    if [ -f VERSION ]; then
        echo "   $(cat VERSION)"
    else
        echo "   Aucune version trouvée"
    fi
    exit 1
fi

NEW_VERSION=$1

# Validation du format de version (semantic versioning)
if ! [[ $NEW_VERSION =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "❌ Format de version invalide: $NEW_VERSION"
    echo "✅ Format attendu: MAJOR.MINOR.PATCH (ex: 1.2.3)"
    exit 1
fi

# Afficher la version actuelle
if [ -f VERSION ]; then
    CURRENT_VERSION=$(cat VERSION)
    echo "📋 Version actuelle: $CURRENT_VERSION"
else
    echo "📋 Aucune version actuelle trouvée"
    CURRENT_VERSION="none"
fi

echo "🔄 Mise à jour vers: $NEW_VERSION"

# Confirmation
read -p "❓ Confirmer la mise à jour ? (y/N): " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "❌ Mise à jour annulée"
    exit 1
fi

# Mise à jour du fichier VERSION
echo "📝 Mise à jour du fichier VERSION..."
echo "$NEW_VERSION" > VERSION

# Recompilation
echo "🔨 Recompilation..."
if go build -o recondeps recondeps.go; then
    echo "✅ Compilation réussie"
else
    echo "❌ Erreur de compilation"
    # Restaurer l'ancienne version en cas d'erreur
    if [ "$CURRENT_VERSION" != "none" ]; then
        echo "$CURRENT_VERSION" > VERSION
        echo "🔄 Version restaurée: $CURRENT_VERSION"
    fi
    exit 1
fi

# Vérification
echo "🔍 Vérification de la nouvelle version..."
NEW_VERSION_CHECK=$(./recondeps -version | grep -o 'v[0-9]\+\.[0-9]\+\.[0-9]\+' | sed 's/v//')

if [ "$NEW_VERSION_CHECK" = "$NEW_VERSION" ]; then
    echo "✅ Version mise à jour avec succès: $NEW_VERSION"
else
    echo "❌ Erreur: Version attendue $NEW_VERSION, trouvée $NEW_VERSION_CHECK"
    exit 1
fi

# Test rapide
echo "🧪 Test rapide..."
if ./recondeps -help >/dev/null 2>&1; then
    echo "✅ Test de base réussi"
else
    echo "⚠️  Test de base échoué (mais binaire créé)"
fi

echo ""
echo "🎉 Mise à jour terminée!"
echo "📊 Résumé:"
echo "   Ancienne version: $CURRENT_VERSION"
echo "   Nouvelle version: $NEW_VERSION"
echo "   Binaire: ./recondeps"
echo ""
echo "📋 Prochaines étapes suggérées:"
echo "   1. Tester: ./recondeps -url http://localhost:8000"
echo "   2. Documenter les changements dans VERSIONING.md"
echo "   3. Tester sur différents sites"