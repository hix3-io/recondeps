// Patterns de découverte JavaScript inspirés de reconjsx
// Adaptés pour la reconnaissance de dépendances et supply chain analysis

class JSDiscoveryPatterns {
  constructor() {
    // Patterns pour détecter des imports dynamiques cachés dans les strings
    this.dynamicImportPatterns = [
      // Template literals avec imports
      /`.*?import\s*\(\s*['"]([^'"]+)['"]\s*\).*?`/g,
      /`.*?require\s*\(\s*['"]([^'"]+)['"]\s*\).*?`/g,
      
      // String concatenation pour imports
      /['"]([^'"]*[@][^'"]*)['"]\s*\+/g,
      /\+\s*['"]([^'"]*[@][^'"]*)['"]/g,
      
      // Conditional imports
      /require\s*\(\s*condition\s*\?\s*['"]([^'"]+)['"]\s*:/g,
      /import\s*\(\s*condition\s*\?\s*['"]([^'"]+)['"]\s*:/g
    ];
    
    // Patterns pour détecter des références de packages dans les configurations
    this.configPatterns = [
      // Package.json patterns dans le code
      /"dependencies"\s*:\s*{[^}]*"(@[^"]+\/[^"]+)"/g,
      /"devDependencies"\s*:\s*{[^}]*"(@[^"]+\/[^"]+)"/g,
      /"peerDependencies"\s*:\s*{[^}]*"(@[^"]+\/[^"]+)"/g,
      
      // Webpack/bundler configurations
      /externals\s*:\s*{[^}]*['"](@[^'"]+\/[^'"]+)['"]/g,
      /alias\s*:\s*{[^}]*['"](@[^'"]+\/[^'"]+)['"]/g,
      
      // Module federation
      /remotes\s*:\s*{[^}]*['"](@[^'"]+\/[^'"]+)['"]/g,
      /shared\s*:\s*{[^}]*['"](@[^'"]+\/[^'"]+)['"]/g
    ];
    
    // Patterns pour imports encodés ou obfusqués
    this.obfuscatedPatterns = [
      // Base64 encoded package names
      /atob\s*\(\s*['"]([A-Za-z0-9+/=]+)['"]\s*\)/g,
      /Buffer\.from\s*\(\s*['"]([A-Za-z0-9+/=]+)['"]\s*,\s*['"]base64['"]\s*\)/g,
      
      // Hex encoded
      /Buffer\.from\s*\(\s*['"]([0-9a-fA-F]+)['"]\s*,\s*['"]hex['"]\s*\)/g,
      
      // String character codes (often used in obfuscation)
      /String\.fromCharCode\s*\([^)]+\)/g
    ];
    
    // Patterns pour détecter des dépendances conditionnelles
    this.conditionalPatterns = [
      // Try-catch import patterns
      /try\s*{[^}]*require\s*\(\s*['"](@[^'"]+\/[^'"]+)['"]\s*\)/g,
      /try\s*{[^}]*import\s*\(\s*['"](@[^'"]+\/[^'"]+)['"]\s*\)/g,
      
      // Optional chaining avec require
      /require\.resolve\s*\?\.\s*\(\s*['"](@[^'"]+\/[^'"]+)['"]\s*\)/g,
      
      // Environment-based imports
      /process\.env\.[A-Z_]+.*?require\s*\(\s*['"](@[^'"]+\/[^'"]+)['"]/g
    ];
  }

  // Extraction de patterns dynamiques depuis le contenu JavaScript
  extractDynamicImports(content) {
    const results = new Set();
    
    this.dynamicImportPatterns.forEach(pattern => {
      let match;
      while ((match = pattern.exec(content)) !== null) {
        const packageName = match[1];
        if (this.isScopedPackage(packageName)) {
          results.add(packageName);
        }
      }
    });
    
    return Array.from(results);
  }

  // Extraction depuis les configurations
  extractConfigReferences(content) {
    const results = new Set();
    
    this.configPatterns.forEach(pattern => {
      let match;
      while ((match = pattern.exec(content)) !== null) {
        const packageName = match[1];
        if (this.isScopedPackage(packageName)) {
          results.add(packageName);
        }
      }
    });
    
    return Array.from(results);
  }

  // Détection d'imports obfusqués
  extractObfuscatedImports(content) {
    const results = new Set();
    
    // Decode base64 strings and check if they contain scoped packages
    const base64Pattern = /(?:atob|Buffer\.from)\s*\(\s*['"]([A-Za-z0-9+/=]+)['"]/g;
    let match;
    while ((match = base64Pattern.exec(content)) !== null) {
      try {
        const decoded = Buffer.from(match[1], 'base64').toString('utf8');
        const scopedMatches = decoded.match(/@[a-zA-Z0-9\-_]+\/[a-zA-Z0-9\-_.]+/g);
        if (scopedMatches) {
          scopedMatches.forEach(pkg => results.add(pkg));
        }
      } catch (e) {
        // Ignore invalid base64
      }
    }
    
    return Array.from(results);
  }

  // Extraction d'imports conditionnels
  extractConditionalImports(content) {
    const results = new Set();
    
    this.conditionalPatterns.forEach(pattern => {
      let match;
      while ((match = pattern.exec(content)) !== null) {
        const packageName = match[1];
        if (this.isScopedPackage(packageName)) {
          results.add(packageName);
        }
      }
    });
    
    return Array.from(results);
  }

  // Validation si c'est un package scoped
  isScopedPackage(packageName) {
    if (!packageName || typeof packageName !== 'string') return false;
    
    // Doit commencer par @
    if (!packageName.startsWith('@')) return false;
    
    // Ne doit pas être un chemin relatif
    if (packageName.startsWith('./') || packageName.startsWith('../')) return false;
    
    // Doit correspondre au format @org/package
    const scopedPattern = /^@[a-zA-Z0-9\-_]+\/[a-zA-Z0-9\-_.]+/;
    return scopedPattern.test(packageName);
  }

  // Méthode principale pour extraire tous les types de patterns
  extractAllPatterns(content) {
    const allResults = new Set();
    
    // Extraire tous les types de patterns
    const dynamicImports = this.extractDynamicImports(content);
    const configRefs = this.extractConfigReferences(content);
    const obfuscatedImports = this.extractObfuscatedImports(content);
    const conditionalImports = this.extractConditionalImports(content);
    
    // Combiner tous les résultats
    [...dynamicImports, ...configRefs, ...obfuscatedImports, ...conditionalImports]
      .forEach(pkg => allResults.add(pkg));
    
    return {
      total: Array.from(allResults),
      dynamic: dynamicImports,
      config: configRefs,
      obfuscated: obfuscatedImports,
      conditional: conditionalImports
    };
  }

  // Analyse de risque basée sur les patterns détectés
  analyzeRisk(patterns) {
    const risks = [];
    
    if (patterns.obfuscated.length > 0) {
      risks.push({
        level: 'HIGH',
        type: 'Obfuscated Dependencies',
        description: `Found ${patterns.obfuscated.length} potentially obfuscated package references`,
        packages: patterns.obfuscated
      });
    }
    
    if (patterns.dynamic.length > 0) {
      risks.push({
        level: 'MEDIUM',
        type: 'Dynamic Dependencies',
        description: `Found ${patterns.dynamic.length} dynamically loaded dependencies`,
        packages: patterns.dynamic
      });
    }
    
    if (patterns.conditional.length > 0) {
      risks.push({
        level: 'MEDIUM',
        type: 'Conditional Dependencies', 
        description: `Found ${patterns.conditional.length} conditionally loaded dependencies`,
        packages: patterns.conditional
      });
    }
    
    if (patterns.config.length > 0) {
      risks.push({
        level: 'LOW',
        type: 'Configuration References',
        description: `Found ${patterns.config.length} dependencies referenced in configuration`,
        packages: patterns.config
      });
    }
    
    return risks;
  }
}

module.exports = { JSDiscoveryPatterns };