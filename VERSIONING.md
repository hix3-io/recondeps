# Guide de Versioning - ReconDeps

## 📋 **Système de versioning**

ReconDeps utilise le **Semantic Versioning** (SemVer) : `MAJOR.MINOR.PATCH`

```
0.1.0
│ │ │
│ │ └── PATCH: Bug fixes, petites améliorations
│ └──── MINOR: Nouvelles fonctionnalités, rétrocompatibles  
└────── MAJOR: Breaking changes, refactoring majeur
```

## 🔧 **Comment mettre à jour la version**

### **1. Modifier le fichier VERSION**

```bash
# Version actuelle
cat VERSION
# 0.1.0

# Mettre à jour (exemple: nouvelle fonctionnalité)
echo "0.2.0" > VERSION
```

### **2. Recompiler l'outil**

```bash
# Le code lit automatiquement le fichier VERSION
go build -o recondeps recondeps.go
```

### **3. Vérifier la nouvelle version**

```bash
./recondeps -version
# ReconDeps v0.2.0 - Dependency Reconnaissance Tool
```

## 📈 **Types de mises à jour**

### **PATCH (0.1.0 → 0.1.1)**
- Corrections de bugs
- Améliorations de performance  
- Corrections de documentation
- Petites optimisations

**Exemple :**
```bash
echo "0.1.1" > VERSION
go build -o recondeps recondeps.go
```

### **MINOR (0.1.1 → 0.2.0)**
- Nouvelles fonctionnalités
- Nouveaux patterns de détection
- Nouvelles options CLI
- Améliorations rétrocompatibles

**Exemple :**
```bash
echo "0.2.0" > VERSION
go build -o recondeps recondeps.go
```

### **MAJOR (0.2.0 → 1.0.0)**
- Breaking changes dans l'API
- Refactoring complet
- Changements incompatibles
- Nouvelles architectures

**Exemple :**
```bash
echo "1.0.0" > VERSION
go build -o recondeps recondeps.go
```

## 🏷️ **Changelog des versions**

### **v0.1.0** (2025-10-27)
- ✨ Version initiale
- 🔍 Crawling de sites web pour fichiers JS
- 📦 Extraction de dépendances (ES6, CommonJS, dynamic)
- 🎯 Détection packages scoped (@org/package)
- 🚨 Détection d'obfuscation (base64)
- 📊 Sortie simplifiée listant toutes les dépendances

### **Exemple de prochaines versions :**

**v0.1.1** - Bug fixes
- 🐛 Correction parsing base64
- ⚡ Amélioration performance regex
- 📝 Documentation améliorée

**v0.2.0** - Nouvelles fonctionnalités  
- 🍪 Support cookies/authentification
- 🕷️ Crawling récursif profond
- 🔍 Nouveaux patterns détection
- 📈 Métriques détaillées

**v1.0.0** - Version stable
- 🏗️ Architecture finalisée
- 🧪 Tests complets
- 📚 Documentation complète
- 🔒 Sécurité renforcée

## 🚀 **Workflow de release**

### **1. Développement**
```bash
# Faire les modifications dans recondeps.go
vim recondeps.go

# Tester les changements
go build -o recondeps recondeps.go
./recondeps -url http://localhost:8000
```

### **2. Mise à jour de version**
```bash
# Choisir le nouveau numéro de version
echo "0.1.1" > VERSION

# Recompiler
go build -o recondeps recondeps.go

# Vérifier
./recondeps -version
```

### **3. Documentation**
```bash
# Mettre à jour ce fichier VERSIONING.md
# Ajouter l'entrée dans le changelog
# Documenter les nouvelles fonctionnalités
```

### **4. Tests**
```bash
# Tester sur différents sites
./recondeps -url https://example.com
./recondeps -url http://localhost:8000 -debug
./recondeps -url https://target.com -json
```

## 📂 **Structure des fichiers versioning**

```
recondependencies/
├── VERSION              # Numéro de version actuel
├── VERSIONING.md        # Ce guide
├── recondeps.go         # Code source (lit VERSION)
├── recondeps            # Binaire compilé
└── CHANGELOG.md         # Historique détaillé (optionnel)
```

## 🎯 **Bonnes pratiques**

### **✅ À faire :**
- Incrémenter la version pour chaque changement
- Tester après chaque mise à jour
- Documenter les nouvelles fonctionnalités
- Garder un historique des versions

### **❌ À éviter :**
- Modifier le code sans incrémenter la version
- Sauter des numéros de version
- Oublier de recompiler après mise à jour VERSION
- Breaking changes dans une version MINOR/PATCH

## 🔄 **Exemple complet de mise à jour**

```bash
# 1. État actuel
./recondeps -version
# ReconDeps v0.1.0

# 2. Ajouter une nouvelle fonctionnalité
echo "// Nouvelle fonctionnalité" >> recondeps.go

# 3. Incrémenter version (nouvelle fonctionnalité = MINOR)
echo "0.2.0" > VERSION

# 4. Recompiler
go build -o recondeps recondeps.go

# 5. Vérifier
./recondeps -version
# ReconDeps v0.2.0

# 6. Tester
./recondeps -url http://localhost:8000

# 7. Documenter dans VERSIONING.md
# Ajouter l'entrée v0.2.0 dans le changelog
```

## 📊 **Suivi des versions**

| Version | Date | Type | Description |
|---------|------|------|-------------|
| 0.1.0 | 2025-10-27 | Initial | Version initiale avec crawling et extraction |
| 0.1.1 | TBD | Patch | Corrections bugs |
| 0.2.0 | TBD | Minor | Nouvelles fonctionnalités |
| 1.0.0 | TBD | Major | Version stable |

---

**Note :** Ce système garantit que chaque version de ReconDeps est identifiable et traceable, facilitant le debugging et le déploiement.