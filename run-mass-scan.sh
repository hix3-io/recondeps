#!/bin/bash

# Script wrapper pour mass scanning avec ReconDeps
# Usage: ./run-mass-scan.sh domains.txt [options]

set -e

if [ $# -eq 0 ]; then
    echo "❌ Usage: $0 <domains_file> [options]"
    echo ""
    echo "📋 Examples:"
    echo "   $0 31k_domains.txt"
    echo "   $0 targets.txt --workers 100 --timeout 15s"
    echo "   $0 scope.txt --only-scoped --workers 200"
    echo ""
    echo "⚙️ Options:"
    echo "   --workers N      Number of concurrent workers (default: CPU*4)"
    echo "   --timeout Xs     Timeout per domain in seconds (default: 30s)"
    echo "   --only-scoped    Only save results with scoped packages"
    echo "   --https-first    Try HTTPS before HTTP (default: true)"
    echo "   --progress Ns    Progress update interval (default: 10s)"
    exit 1
fi

DOMAINS_FILE="$1"
shift

# Valeurs par défaut
WORKERS=$(($(nproc) * 4))
TIMEOUT="30s"
ONLY_SCOPED=false
HTTPS_FIRST=true
PROGRESS_INTERVAL=10
OUTPUT_DIR="mass_scan_$(date +%Y%m%d_%H%M%S)"

# Parser les options
while [[ $# -gt 0 ]]; do
    case $1 in
        --workers)
            WORKERS="$2"
            shift 2
            ;;
        --timeout)
            TIMEOUT="$2"
            shift 2
            ;;
        --only-scoped)
            ONLY_SCOPED=true
            shift
            ;;
        --https-first)
            HTTPS_FIRST=true
            shift
            ;;
        --progress)
            PROGRESS_INTERVAL="$2"
            shift 2
            ;;
        *)
            echo "❌ Unknown option: $1"
            exit 1
            ;;
    esac
done

# Vérifier que le fichier existe
if [ ! -f "$DOMAINS_FILE" ]; then
    echo "❌ Domains file not found: $DOMAINS_FILE"
    exit 1
fi

# Compter les domaines
TOTAL_DOMAINS=$(wc -l < "$DOMAINS_FILE")

echo "🚀 ReconDeps Mass Scanner Starting"
echo "=================================="
echo "📁 Domains file: $DOMAINS_FILE"
echo "📊 Total domains: $TOTAL_DOMAINS"
echo "⚙️  Workers: $WORKERS"
echo "⏱️  Timeout: $TIMEOUT"
echo "📁 Output directory: $OUTPUT_DIR"
echo "🎯 Only scoped packages: $ONLY_SCOPED"
echo ""

# Créer les dossiers de sortie
mkdir -p "$OUTPUT_DIR"
mkdir -p "$OUTPUT_DIR/high_value_targets"
mkdir -p "$OUTPUT_DIR/logs"

# Fonctions pour les stats
START_TIME=$(date +%s)
PROCESSED=0
SUCCESSFUL=0
WITH_JS=0
WITH_SCOPED=0

update_stats() {
    local current_time=$(date +%s)
    local elapsed=$((current_time - START_TIME))
    local rate=0
    if [ $elapsed -gt 0 ]; then
        rate=$((PROCESSED / elapsed))
    fi
    
    echo "📊 Processed: $PROCESSED/$TOTAL_DOMAINS | Success: $SUCCESSFUL | JS: $WITH_JS | Scoped: $WITH_SCOPED | Rate: ${rate}/s"
}

# Fonction pour scanner un domaine
scan_domain() {
    local domain="$1"
    local result_file="$OUTPUT_DIR/${domain}.json"
    local log_file="$OUTPUT_DIR/logs/${domain}.log"
    
    # Essayer HTTPS d'abord, puis HTTP
    local urls=()
    if [ "$HTTPS_FIRST" = true ]; then
        urls=("https://$domain" "http://$domain")
    else
        urls=("http://$domain" "https://$domain")
    fi
    
    for url in "${urls[@]}"; do
        if timeout ${TIMEOUT%s} ./recondeps -url "$url" -json > "$result_file" 2> "$log_file"; then
            # Analyser les résultats
            local js_files=$(jq -r '.summary.js_files_analyzed // 0' "$result_file" 2>/dev/null || echo "0")
            local scoped_pkgs=$(jq -r '.summary.scoped_packages // 0' "$result_file" 2>/dev/null || echo "0")
            
            if [ "$js_files" -gt 0 ]; then
                ((WITH_JS++))
            fi
            
            if [ "$scoped_pkgs" -gt 0 ]; then
                ((WITH_SCOPED++))
                # Copier vers high value targets
                cp "$result_file" "$OUTPUT_DIR/high_value_targets/"
                echo "🎯 HIGH VALUE: $domain ($scoped_pkgs scoped packages)"
            fi
            
            # Supprimer si only-scoped et pas de packages scoped
            if [ "$ONLY_SCOPED" = true ] && [ "$scoped_pkgs" -eq 0 ]; then
                rm "$result_file"
            fi
            
            ((SUCCESSFUL++))
            break
        else
            # Si l'URL échoue, continuer avec la suivante
            continue
        fi
    done
    
    # Si aucune URL n'a fonctionné, nettoyer
    if [ ! -s "$result_file" ]; then
        rm -f "$result_file" "$log_file"
    fi
    
    ((PROCESSED++))
}

# Export des fonctions pour GNU parallel
export -f scan_domain update_stats
export OUTPUT_DIR ONLY_SCOPED HTTPS_FIRST TIMEOUT WITH_JS WITH_SCOPED SUCCESSFUL PROCESSED

# Vérifier si GNU parallel est disponible
if command -v parallel >/dev/null 2>&1; then
    echo "🚀 Using GNU parallel for maximum performance"
    
    # Lancer le scan avec GNU parallel
    cat "$DOMAINS_FILE" | parallel -j "$WORKERS" --progress scan_domain
    
else
    echo "⚡ Using xargs (install GNU parallel for better performance)"
    
    # Fallback avec xargs
    cat "$DOMAINS_FILE" | xargs -n 1 -P "$WORKERS" -I {} bash -c 'scan_domain "$@"' _ {}
fi

# Stats finales
END_TIME=$(date +%s)
TOTAL_TIME=$((END_TIME - START_TIME))

echo ""
echo "🎉 Mass scan completed!"
echo "======================"
echo "📊 Total domains: $TOTAL_DOMAINS"
echo "✅ Processed: $PROCESSED"
echo "🟢 Successful: $SUCCESSFUL"
echo "📁 With JavaScript: $WITH_JS"
echo "🎯 With scoped packages: $WITH_SCOPED"
echo "⏱️  Total time: ${TOTAL_TIME}s"
echo "⚡ Average rate: $((PROCESSED / TOTAL_TIME))/s"
echo ""
echo "📁 Results saved in: $OUTPUT_DIR"

if [ "$WITH_SCOPED" -gt 0 ]; then
    echo ""
    echo "🚨 HIGH VALUE TARGETS FOUND:"
    find "$OUTPUT_DIR/high_value_targets" -name "*.json" | while read -r file; do
        domain=$(basename "$file" .json)
        scoped_count=$(jq -r '.summary.scoped_packages // 0' "$file" 2>/dev/null || echo "0")
        echo "  🎯 $domain: $scoped_count scoped packages"
    done
    echo ""
    echo "⚠️  POTENTIAL SUPPLY CHAIN ATTACK VECTORS IDENTIFIED!"
fi

# Créer un résumé JSON
cat > "$OUTPUT_DIR/scan_summary.json" << EOF
{
  "scan_metadata": {
    "total_domains": $TOTAL_DOMAINS,
    "processed_domains": $PROCESSED,
    "successful_scans": $SUCCESSFUL,
    "scan_duration_seconds": $TOTAL_TIME,
    "domains_per_second": $((PROCESSED / TOTAL_TIME)),
    "timestamp": "$(date -Iseconds)"
  },
  "findings": {
    "domains_with_javascript": $WITH_JS,
    "domains_with_scoped_packages": $WITH_SCOPED,
    "high_value_targets": $WITH_SCOPED
  },
  "configuration": {
    "workers": $WORKERS,
    "timeout": "$TIMEOUT",
    "only_scoped": $ONLY_SCOPED,
    "https_first": $HTTPS_FIRST
  }
}
EOF

echo "📊 Summary saved to: $OUTPUT_DIR/scan_summary.json"