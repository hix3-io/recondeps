#!/bin/bash

# ReconDeps Quick Start Guide
# Pour bug bounty et reconnaissance d'acquisition

echo "🎯 ReconDeps v0.3.0 - Bug Bounty Edition"
echo "========================================"
echo ""

# Vérifier que les binaires existent
if [ ! -f "./recondeps" ]; then
    echo "⚠️  Binary not found. Compiling..."
    go build -o recondeps recondeps.go
    echo "✅ Compiled successfully"
fi

echo "📋 Quick Start Options:"
echo ""
echo "1️⃣  Single domain scan:"
echo "   ./recondeps -url https://target.com"
echo ""
echo "2️⃣  Single domain with debug:"
echo "   ./recondeps -url https://target.com -debug"
echo ""
echo "3️⃣  Mass scan (small scope):"
echo "   ./run-mass-scan.sh domains.txt --workers 10"
echo ""
echo "4️⃣  Mass scan (large scope - 31k+ domains):"
echo "   ./run-mass-scan.sh huge_scope.txt --workers 200 --only-scoped"
echo ""
echo "5️⃣  Test with provided targets:"
echo "   ./run-mass-scan.sh test-domains.txt --workers 5"
echo ""

read -p "🔍 Choose option (1-5) or Enter to skip: " choice

case $choice in
    1)
        read -p "🌐 Enter target URL: " url
        echo "🚀 Scanning $url..."
        ./recondeps -url "$url"
        ;;
    2)
        read -p "🌐 Enter target URL: " url
        echo "🚀 Scanning $url with debug..."
        ./recondeps -url "$url" -debug
        ;;
    3)
        if [ -f "test-domains.txt" ]; then
            echo "🚀 Starting small mass scan..."
            ./run-mass-scan.sh test-domains.txt --workers 10
        else
            echo "❌ test-domains.txt not found. Create it first with your domains."
        fi
        ;;
    4)
        read -p "📁 Enter path to your large domains file: " domains_file
        if [ -f "$domains_file" ]; then
            echo "🚀 Starting large-scale mass scan..."
            echo "⚠️  This will use 200 workers and high performance settings"
            read -p "Continue? (y/N): " confirm
            if [[ $confirm =~ ^[Yy]$ ]]; then
                ./run-mass-scan.sh "$domains_file" --workers 200 --only-scoped --timeout 15s
            fi
        else
            echo "❌ File not found: $domains_file"
        fi
        ;;
    5)
        if [ -f "test-domains.txt" ]; then
            echo "🚀 Testing with provided targets..."
            ./run-mass-scan.sh test-domains.txt --workers 5 --timeout 15s
        else
            echo "❌ test-domains.txt not found"
        fi
        ;;
    *)
        echo "ℹ️  Skipping quick start"
        ;;
esac

echo ""
echo "📚 Documentation:"
echo "   📖 Main README: README-GO.md"
echo "   🎯 Bug Bounty Guide: README-BUGBOUNTY.md"
echo "   🔧 Versioning: VERSIONING.md"
echo ""
echo "🚀 Happy Bug Bounty Hunting!"
echo ""
echo "💡 Pro Tip: Use --only-scoped flag to focus on high-value targets"
echo "⚡ Performance: 200+ domains/sec with proper configuration"