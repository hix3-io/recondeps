# ReconDependencies

**JavaScript Dependency Reconnaissance Tool for Supply Chain Analysis**

Outil d'analyse de dépendances JavaScript inspiré de `reconjsx`, utilisant l'analyse AST avec SWC pour découvrir les packages scoped et identifier les vecteurs potentiels d'attaque de supply chain.

## 🎯 Objectifs

- **Reconnaissance de dépendances** : Extraire tous les imports JavaScript (ES6, CommonJS, dynamiques)
- **Analyse de supply chain** : Identifier les packages privés/scoped (@org/package)
- **Détection avancée** : Patterns obfusqués, imports conditionnels, références de configuration
- **Analyse de risque** : Évaluation des vecteurs d'attaque potentiels

## 🚀 Installation

```bash
cd recondependencies
npm install
```

## 📋 Utilisation

### Analyse basique
```bash
node index.js ./mon-projet
```

### Analyse avec debug
```bash
node index.js --debug ./mon-projet
```

### Export JSON
```bash
node index.js --json ./mon-projet > results.json
```

### Sauvegarde des résultats
```bash
node index.js --output rapport.json ./mon-projet
```

## 🔍 Fonctionnalités

### Extraction d'imports
- **ES6 imports** : `import { X } from '@org/package'`
- **CommonJS** : `const X = require('@org/package')`
- **Dynamic imports** : `await import('@org/package')`
- **TypeScript/JSX** : Support complet

### Patterns avancés (inspirés de reconjsx)
- **Imports dynamiques** : Template literals, concaténation
- **Configuration** : package.json, webpack.config.js, externals
- **Obfuscation** : Base64, hex encoding, String.fromCharCode
- **Imports conditionnels** : try/catch, environment-based

### Analyse de risque
- **HIGH** : Dépendances obfusquées
- **MEDIUM** : Imports dynamiques/conditionnels
- **LOW** : Références de configuration

## 📊 Format de sortie

```json
{
  "summary": {
    "files_processed": 247,
    "total_imports": 1453,
    "scoped_packages_found": 23,
    "organizations_found": 5,
    "advanced_patterns": {
      "dynamic_imports": 12,
      "obfuscated_imports": 3,
      "conditional_imports": 8
    }
  },
  "scoped_packages": [
    "@acquisition-utils/auth",
    "@legacy-company/components"
  ],
  "organizations": [
    "@acquisition-utils",
    "@legacy-company"
  ],
  "advanced_findings": {
    "dynamic": ["@company/dynamic-loader"],
    "obfuscated": ["@company/secret-module"],
    "conditional": ["@company/optional-feature"]
  },
  "risk_analysis": [
    {
      "level": "HIGH",
      "type": "Obfuscated Dependencies",
      "description": "Found 3 potentially obfuscated package references"
    }
  ]
}
```

## 🧪 Tests

```bash
node test.js
```

Le script de test crée automatiquement des fichiers d'exemple et valide toutes les fonctionnalités.

## 🔧 Architecture

### Modules principaux

- **`index.js`** : Scanner principal avec analyse AST (SWC)
- **`patterns.js`** : Patterns de découverte avancés (inspirés de reconjsx)
- **`test.js`** : Suite de tests complète

### Inspiration reconjsx

L'outil s'inspire de l'architecture de `reconjsx` pour :
- **Discovery patterns** : Regex avancées pour extraire les dépendances
- **Performance** : Traitement parallèle et cache
- **Filtrage intelligent** : Exclusion des patterns non pertinents
- **Analyse de risque** : Catégorisation des trouvailles

## 🛡️ Cas d'usage sécurité

### Supply Chain Analysis
```bash
# Analyser un projet pour identifier les vecteurs d'attaque
node index.js --debug ./target-project

# Rechercher des dépendances suspectes
node index.js --json ./target-project | jq '.advanced_findings.obfuscated'
```

### Audit de dépendances
```bash
# Lister toutes les organisations
node index.js ./project | grep "Organizations found" -A 10

# Identifier les packages privés
node index.js --json ./project | jq '.scoped_packages[]'
```

## ⚠️ Avertissement

Cet outil est destiné à la **recherche en sécurité défensive** uniquement. Assurez-vous d'avoir l'autorisation appropriée avant d'analyser tout code.

## 🎯 Exemple de sortie

```
[*] Found 247 JavaScript files
[*] Extracted 1,453 imports total
[*] Found 23 scoped packages
[*] Advanced patterns: 12 dynamic, 3 obfuscated, 8 conditional

[+] Private/scoped packages:
  @acquisition-utils/auth
  @acquisition-utils/api-client
  @legacy-company/shared-components

[+] Organizations found:
  - @acquisition-utils
  - @legacy-company

🚨 Obfuscated dependencies detected:
  ⚠️  @company/secret-module

🛡️ Risk Analysis:
  [HIGH] Obfuscated Dependencies: Found 3 potentially obfuscated package references

⚠️ Potential targets for supply chain attack
```

## 🔗 Comparaison avec reconjsx

| Fonctionnalité | reconjsx | recondependencies |
|---|---|---|
| **Analyse JS** | Endpoints/routes | Dépendances/imports |
| **Parser** | Regex patterns | AST + Regex patterns |
| **Cible** | Surface d'attaque web | Supply chain |
| **Sortie** | URLs/endpoints | Packages scoped |
| **Obfuscation** | Secrets detection | Import obfuscation |

Les deux outils partagent la même philosophie de reconnaissance exhaustive mais avec des objectifs différents.