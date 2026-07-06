# ReconDeps - Go Version

**JavaScript Dependency Reconnaissance Tool for Supply Chain Analysis**

Outil Go inspiré de l'architecture de `reconjsx` qui analyse les dépendances JavaScript d'un site web pour détecter les vecteurs potentiels d'attaque de supply chain.

## 🎯 **Différence avec la version Node.js**

| Aspect | Version Node.js | Version Go |
|---|---|---|
| **Input** | Dossier local | URL web |
| **Workflow** | Analyse fichiers locaux | Crawl → Download → Analyse |
| **Architecture** | Comme un linter | Comme reconjsx |
| **Usage** | Audit code source | Reconnaissance web |

## 🚀 **Installation et Compilation**

```bash
# Compiler l'outil
go build -o recondeps recondeps.go

# Rendre exécutable
chmod +x recondeps

# Optionnel: Installation globale
sudo mv recondeps /usr/local/bin/
```

## 📋 **Utilisation**

### **Commandes de base :**

```bash
# Scan simple d'un site
./recondeps -url https://example.com

# Avec debug détaillé
./recondeps -url https://example.com -debug

# Export JSON
./recondeps -url https://example.com -json

# Sauvegarde des résultats
./recondeps -url https://example.com -output analysis.json
```

### **Options avancées :**

```bash
# Contrôler la profondeur de crawling
./recondeps -url https://example.com -depth 5

# Ajouter un délai entre requêtes
./recondeps -url https://example.com -delay 500ms

# Version et aide
./recondeps -version
./recondeps -help
```

## 🔍 **Fonctionnalités**

### **Découverte de fichiers JS (comme reconjsx)**
- Parsing HTML pour détecter les `<script src="...">`
- Découverte d'imports dynamiques
- Crawling récursif des assets JavaScript

### **Extraction de dépendances**
- **ES6 imports** : `import { X } from '@org/package'`
- **CommonJS** : `const X = require('@org/package')`
- **Dynamic imports** : `await import('@org/package')`
- **Imports conditionnels** : try/catch, environment-based
- **Configuration** : webpack externals, aliases

### **Détection avancée**
- **Obfuscation** : Base64, Buffer encoding
- **Patterns cachés** : String concatenation, template literals
- **Supply chain risks** : Focus sur packages scoped (@org/package)

## 📊 **Exemple de sortie**

```
🔍 Starting dependency reconnaissance scan
[*] Discovering JavaScript files from: https://target.com
[*] Found 12 JavaScript files
[*] Target: https://target.com
[*] JavaScript files analyzed: 12
[*] Dependencies found: 89
[*] Scoped packages: 23
[*] Organizations: 8
[*] Scan duration: 2.1s

[+] Organizations found:
  - @company
  - @security-team
  - @analytics
  - @malware

[+] Scoped packages found:
  @company/shared-utils
  @security-team/auth-service
  @analytics/tracking-lib
  @malware/data-exfiltration

🛡️ Risk Analysis:
  [HIGH] Obfuscated Dependencies: Found 3 potentially obfuscated package references
  [MEDIUM] Scoped/Conditional Dependencies: Found 23 scoped or conditional dependencies

⚠️ Potential targets for supply chain attack
```

## 🛡️ **Cas d'usage sécurité**

### **Reconnaissance d'acquisition**
```bash
# Analyser les dépendances d'une entreprise cible
./recondeps -url https://target-company.com -output acquisition-deps.json

# Identifier les organisations utilisées
./recondeps -url https://target-company.com -json | jq '.organizations[]'
```

### **Audit de sécurité web**
```bash
# Scanner pour des backdoors potentielles
./recondeps -url https://suspicious-site.com -debug | grep -i "malware\|backdoor\|suspicious"

# Analyser les risques supply chain
./recondeps -url https://client-site.com -json | jq '.risks[] | select(.level == "HIGH")'
```

### **Monitoring continu**
```bash
# Script de surveillance (à ajouter en cron)
./recondeps -url https://production-app.com -output "scan-$(date +%Y%m%d).json"
```

## 🔧 **Architecture technique**

### **Inspirée de reconjsx :**
- **Scanner struct** : Client HTTP avec configuration
- **Patterns regex** : Extraction exhaustive comme reconjsx patterns
- **Crawling intelligent** : Découverte récursive des assets
- **Filtrage** : Exclusion des CDN et librairies communes
- **Performance** : Délais configurables, parallélisation

### **Améliorations pour supply chain :**
- **Focus packages scoped** : Priorité aux @org/package
- **Détection d'obfuscation** : Base64, encoding, try/catch
- **Classification de risque** : HIGH/MEDIUM/LOW selon patterns
- **Analyse organisationnelle** : Extraction automatique des entités

## 🚨 **Résultats du test réel**

Sur le site de test local, l'outil a détecté :
- ✅ **25 packages scoped** dans 4 fichiers JS
- ✅ **14 organisations** différentes 
- ✅ **Dépendances suspectes** : `@malware/data-exfiltration`, `@spyware/user-tracking`
- ✅ **Imports conditionnels** cachés dans try/catch
- ✅ **Analyse en 414ms** - Performance excellente

## 🔗 **Comparaison avec l'écosystème**

| Outil | Focus | Input | Sortie |
|---|---|---|---|
| **reconjsx** | Endpoints/routes | URL | Surface d'attaque web |
| **recondeps** | Dépendances | URL | Supply chain risks |
| **npm audit** | Vulnérabilités | package.json | CVE connus |
| **Snyk** | Sécurité | Code source | Vulns + licensing |

## ⚠️ **Avertissement**

Cet outil est destiné à la **recherche en sécurité défensive** uniquement. Assurez-vous d'avoir l'autorisation appropriée avant d'analyser tout site web.

## 🎯 **Prochaines améliorations**

- [ ] Implémentation complète du décodage base64
- [ ] Support des proxies HTTP
- [ ] Authentification (cookies, headers)
- [ ] Crawling plus profond (comme reconjsx crawl mode)
- [ ] Détection de patterns crypto/blockchain
- [ ] Integration avec bases de données de malware

---

**ReconDeps** combine la puissance de reconnaissance de `reconjsx` avec l'analyse de supply chain pour révéler les dépendances cachées et les vecteurs d'attaque potentiels dans les applications web modernes.