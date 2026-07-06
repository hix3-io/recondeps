#!/usr/bin/env node

const { parseSync } = require('@swc/core');
const fs = require('fs');
const path = require('path');
const glob = require('glob');
const minimist = require('minimist');
const { JSDiscoveryPatterns } = require('./patterns.js');

// Configuration inspirée de reconjsx
const CONFIG = {
  extensions: ['.js', '.jsx', '.ts', '.tsx', '.mjs'],
  excludePatterns: [
    '**/node_modules/**',
    '**/dist/**',
    '**/build/**',
    '**/.git/**',
    '**/coverage/**',
    '**/test/**',
    '**/tests/**'
  ],
  maxFileSize: 5 * 1024 * 1024, // 5MB max
  debug: false
};

class DependencyRecon {
  constructor(options = {}) {
    this.debug = options.debug || false;
    this.scopedPackages = new Set();
    this.allImports = new Set();
    this.organizations = new Set();
    this.filesProcessed = 0;
    this.importsFound = 0;
    this.errors = 0;
    this.patterns = new JSDiscoveryPatterns();
    this.advancedFindings = {
      dynamic: new Set(),
      config: new Set(),
      obfuscated: new Set(),
      conditional: new Set()
    };
  }

  log(message, type = 'info') {
    const colors = {
      info: '\x1b[36m[*]\x1b[0m',
      success: '\x1b[32m[+]\x1b[0m', 
      warning: '\x1b[33m[!]\x1b[0m',
      error: '\x1b[31m[-]\x1b[0m',
      debug: '\x1b[35m[DEBUG]\x1b[0m'
    };
    
    if (type === 'debug' && !this.debug) return;
    console.log(`${colors[type]} ${message}`);
  }

  // Méthode inspirée des patterns de reconjsx pour découvrir les fichiers JS
  discoverJavaScriptFiles(targetPath) {
    this.log(`Scanning for JavaScript files in: ${targetPath}`);
    
    const patterns = CONFIG.extensions.map(ext => 
      `${targetPath}/**/*${ext}`
    );
    
    let allFiles = [];
    
    for (const pattern of patterns) {
      try {
        const files = glob.sync(pattern, {
          ignore: CONFIG.excludePatterns,
          nodir: true
        });
        allFiles = allFiles.concat(files);
      } catch (error) {
        this.log(`Error with pattern ${pattern}: ${error.message}`, 'error');
      }
    }
    
    // Dédupliquer et filtrer par taille
    const uniqueFiles = [...new Set(allFiles)];
    const validFiles = uniqueFiles.filter(file => {
      try {
        const stats = fs.statSync(file);
        return stats.size <= CONFIG.maxFileSize;
      } catch {
        return false;
      }
    });
    
    this.log(`Found ${validFiles.length} JavaScript files`);
    return validFiles;
  }

  // Parser AST inspiré des techniques de reconjsx mais orienté extraction d'imports
  extractImportsFromAST(ast) {
    const imports = [];
    
    const traverse = (node) => {
      if (!node || typeof node !== 'object') return;
      
      try {
        // ES6 import statements
        if (node.type === 'ImportDeclaration' && node.source?.value) {
          imports.push(node.source.value);
          if (this.debug) {
            this.log(`ES6 import: ${node.source.value}`, 'debug');
          }
        }
        
        // CommonJS require()
        if (node.type === 'CallExpression' && 
            node.callee?.type === 'Identifier' && 
            node.callee.name === 'require' &&
            node.arguments?.[0]?.type === 'StringLiteral') {
          imports.push(node.arguments[0].value);
          if (this.debug) {
            this.log(`CommonJS require: ${node.arguments[0].value}`, 'debug');
          }
        }
        
        // Dynamic import()
        if (node.type === 'CallExpression' && 
            node.callee?.type === 'Import' &&
            node.arguments?.[0]?.type === 'StringLiteral') {
          imports.push(node.arguments[0].value);
          if (this.debug) {
            this.log(`Dynamic import: ${node.arguments[0].value}`, 'debug');
          }
        }
        
        // Traverse child nodes
        for (const key in node) {
          if (node.hasOwnProperty(key)) {
            const child = node[key];
            if (Array.isArray(child)) {
              child.forEach(traverse);
            } else if (child && typeof child === 'object') {
              traverse(child);
            }
          }
        }
      } catch (error) {
        if (this.debug) {
          this.log(`Error traversing node: ${error.message}`, 'debug');
        }
      }
    };
    
    traverse(ast);
    return imports;
  }

  // Extraction d'imports depuis le code JavaScript (méthode principale)
  extractImports(jsCode, filePath) {
    try {
      // Configuration SWC pour supporter ES6+, JSX, TypeScript
      const config = {
        syntax: 'ecmascript',
        jsx: true,
        dynamicImport: true,
        privateMethod: true,
        functionBind: true,
        exportDefaultFrom: true,
        exportNamespaceFrom: true,
        decorators: true,
        decoratorsBeforeExport: true,
        topLevelAwait: true,
        importMeta: true
      };
      
      // Détecter TypeScript
      if (filePath.endsWith('.ts') || filePath.endsWith('.tsx')) {
        config.syntax = 'typescript';
        config.tsx = filePath.endsWith('.tsx');
      }
      
      const ast = parseSync(jsCode, config);
      const imports = this.extractImportsFromAST(ast);
      
      if (this.debug && imports.length > 0) {
        this.log(`Extracted ${imports.length} imports from ${path.basename(filePath)}`, 'debug');
      }
      
      return imports;
      
    } catch (error) {
      this.errors++;
      if (this.debug) {
        this.log(`Parse error in ${filePath}: ${error.message}`, 'debug');
      }
      return [];
    }
  }

  // Filtrage des packages scoped (logique métier inspirée des filtres de reconjsx)
  filterScopedPackages(imports) {
    const scoped = imports.filter(pkg => {
      // Ne garder que les packages scoped (@org/package)
      if (!pkg.startsWith('@')) return false;
      
      // Exclure les chemins relatifs
      if (pkg.startsWith('./') || pkg.startsWith('../')) return false;
      
      // Valider le format @org/package
      const scopedPattern = /^@[a-zA-Z0-9\-_]+\/[a-zA-Z0-9\-_.]+/;
      return scopedPattern.test(pkg);
    });
    
    return scoped;
  }

  // Extraction des organisations depuis les packages scoped
  extractOrganizations(scopedPackages) {
    const orgs = scopedPackages.map(pkg => {
      const match = pkg.match(/^@([^/]+)\//);
      return match ? `@${match[1]}` : null;
    }).filter(Boolean);
    
    return [...new Set(orgs)];
  }

  // Méthode principale de scan (architecture inspirée de reconjsx)
  async scanDirectory(targetPath) {
    this.log('🔍 Starting dependency reconnaissance scan');
    
    if (!fs.existsSync(targetPath)) {
      this.log(`Target path does not exist: ${targetPath}`, 'error');
      return null;
    }
    
    // Phase 1: Découverte des fichiers JS (comme reconjsx découvre les fichiers JS)
    const jsFiles = this.discoverJavaScriptFiles(targetPath);
    
    if (jsFiles.length === 0) {
      this.log('No JavaScript files found', 'warning');
      return this.generateReport();
    }
    
    // Phase 2: Extraction des imports depuis chaque fichier
    for (const filePath of jsFiles) {
      try {
        const content = fs.readFileSync(filePath, 'utf8');
        
        // Extraction AST classique
        const imports = this.extractImports(content, filePath);
        
        // Extraction avec patterns avancés (inspiré de reconjsx)
        const advancedPatterns = this.patterns.extractAllPatterns(content);
        
        // Combiner tous les imports
        imports.forEach(imp => {
          this.allImports.add(imp);
          this.importsFound++;
        });
        
        // Stocker les résultats avancés
        advancedPatterns.dynamic.forEach(pkg => this.advancedFindings.dynamic.add(pkg));
        advancedPatterns.config.forEach(pkg => this.advancedFindings.config.add(pkg));
        advancedPatterns.obfuscated.forEach(pkg => this.advancedFindings.obfuscated.add(pkg));
        advancedPatterns.conditional.forEach(pkg => this.advancedFindings.conditional.add(pkg));
        
        // Ajouter aussi les patterns avancés aux imports totaux
        advancedPatterns.total.forEach(pkg => {
          this.allImports.add(pkg);
          this.importsFound++;
        });
        
        this.filesProcessed++;
        
        if (this.debug && this.filesProcessed % 50 === 0) {
          this.log(`Processed ${this.filesProcessed}/${jsFiles.length} files`, 'debug');
        }
        
        if (this.debug && advancedPatterns.total.length > 0) {
          this.log(`Advanced patterns found ${advancedPatterns.total.length} additional packages in ${path.basename(filePath)}`, 'debug');
        }
        
      } catch (error) {
        this.errors++;
        if (this.debug) {
          this.log(`Error reading ${filePath}: ${error.message}`, 'debug');
        }
      }
    }
    
    // Phase 3: Filtrage et analyse des dépendances
    const allImportsArray = Array.from(this.allImports);
    const scopedPackages = this.filterScopedPackages(allImportsArray);
    const organizations = this.extractOrganizations(scopedPackages);
    
    scopedPackages.forEach(pkg => this.scopedPackages.add(pkg));
    organizations.forEach(org => this.organizations.add(org));
    
    return this.generateReport();
  }

  // Génération du rapport final
  generateReport() {
    const advancedFindings = {
      dynamic: Array.from(this.advancedFindings.dynamic).sort(),
      config: Array.from(this.advancedFindings.config).sort(),
      obfuscated: Array.from(this.advancedFindings.obfuscated).sort(),
      conditional: Array.from(this.advancedFindings.conditional).sort()
    };
    
    // Analyse de risque
    const riskAnalysis = this.patterns.analyzeRisk(advancedFindings);
    
    const report = {
      summary: {
        files_processed: this.filesProcessed,
        total_imports: this.importsFound,
        scoped_packages_found: this.scopedPackages.size,
        organizations_found: this.organizations.size,
        parse_errors: this.errors,
        advanced_patterns: {
          dynamic_imports: advancedFindings.dynamic.length,
          config_references: advancedFindings.config.length,
          obfuscated_imports: advancedFindings.obfuscated.length,
          conditional_imports: advancedFindings.conditional.length
        }
      },
      scoped_packages: Array.from(this.scopedPackages).sort(),
      organizations: Array.from(this.organizations).sort(),
      advanced_findings: advancedFindings,
      risk_analysis: riskAnalysis,
      timestamp: new Date().toISOString()
    };
    
    return report;
  }

  // Affichage des résultats (style reconjsx)
  displayResults(report) {
    console.log('\\n' + '='.repeat(60));
    this.log(`Found ${report.summary.files_processed} JavaScript files`);
    this.log(`Extracted ${report.summary.total_imports} imports total`);
    this.log(`Found ${report.summary.scoped_packages_found} scoped packages`);
    
    // Affichage des patterns avancés
    const advanced = report.summary.advanced_patterns;
    if (advanced.dynamic_imports > 0 || advanced.obfuscated_imports > 0 || advanced.conditional_imports > 0) {
      this.log(`Advanced patterns: ${advanced.dynamic_imports} dynamic, ${advanced.obfuscated_imports} obfuscated, ${advanced.conditional_imports} conditional`);
    }
    
    if (report.summary.parse_errors > 0) {
      this.log(`Parse errors: ${report.summary.parse_errors}`, 'warning');
    }
    
    console.log('\\n');
    
    if (report.scoped_packages.length > 0) {
      this.log('Private/scoped packages:', 'success');
      report.scoped_packages.forEach(pkg => {
        console.log(`  ${pkg}`);
      });
    }
    
    console.log('\\n');
    
    if (report.organizations.length > 0) {
      this.log('Organizations found:', 'success');
      report.organizations.forEach(org => {
        console.log(`  - ${org}`);
      });
    }
    
    // Affichage des résultats avancés si présents
    if (report.advanced_findings.obfuscated.length > 0) {
      console.log('\\n');
      this.log('🚨 Obfuscated dependencies detected:', 'error');
      report.advanced_findings.obfuscated.forEach(pkg => {
        console.log(`  ⚠️  ${pkg}`);
      });
    }
    
    if (report.advanced_findings.dynamic.length > 0) {
      console.log('\\n');
      this.log('🔄 Dynamic dependencies detected:', 'warning');
      report.advanced_findings.dynamic.forEach(pkg => {
        console.log(`  📦 ${pkg}`);
      });
    }
    
    // Analyse de risque
    if (report.risk_analysis && report.risk_analysis.length > 0) {
      console.log('\\n');
      this.log('🛡️  Risk Analysis:', 'warning');
      report.risk_analysis.forEach(risk => {
        const levelColors = {
          'HIGH': '\\x1b[31m',    // Rouge
          'MEDIUM': '\\x1b[33m', // Jaune
          'LOW': '\\x1b[36m'     // Cyan
        };
        const color = levelColors[risk.level] || '\\x1b[0m';
        console.log(`  ${color}[${risk.level}]\\x1b[0m ${risk.type}: ${risk.description}`);
      });
    }
    
    if (report.scoped_packages.length > 0) {
      console.log('\\n');
      this.log('⚠️  Potential targets for supply chain attack', 'warning');
    }
  }
}

// CLI Interface
async function main() {
  const argv = minimist(process.argv.slice(2));
  
  if (argv.help || argv.h) {
    console.log(`
ReconDependencies - JavaScript Dependency Reconnaissance Tool

Usage: node index.js [options] <target_directory>

Options:
  -d, --debug     Enable debug output
  -j, --json      Output results as JSON
  -o, --output    Save results to file
  -h, --help      Show this help

Examples:
  node index.js ./my-project
  node index.js --debug --json ./my-project
  node index.js --output results.json ./my-project
`);
    return;
  }
  
  const targetPath = argv._[0] || '.';
  const debug = argv.debug || argv.d || false;
  const jsonOutput = argv.json || argv.j || false;
  const outputFile = argv.output || argv.o || null;
  
  const recon = new DependencyRecon({ debug });
  
  try {
    const report = await recon.scanDirectory(targetPath);
    
    if (!report) {
      process.exit(1);
    }
    
    if (jsonOutput) {
      console.log(JSON.stringify(report, null, 2));
    } else {
      recon.displayResults(report);
    }
    
    if (outputFile) {
      fs.writeFileSync(outputFile, JSON.stringify(report, null, 2));
      recon.log(`Results saved to ${outputFile}`, 'success');
    }
    
  } catch (error) {
    console.error(`\\x1b[31m[-] Error: ${error.message}\\x1b[0m`);
    process.exit(1);
  }
}

if (require.main === module) {
  main();
}

module.exports = { DependencyRecon };