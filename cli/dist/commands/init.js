import fs from 'node:fs';
import path from 'node:path';
import { dumpYaml } from '../utils.js';
const DEFAULT_CONFIG = {
    lang: 'typescript',
    outdir: 'src/generated',
};
export function runInit() {
    const root = process.cwd();
    const dirs = ['agents', 'workflows', 'schemas'];
    for (const dir of dirs) {
        const full = path.join(root, dir);
        if (!fs.existsSync(full)) {
            fs.mkdirSync(full, { recursive: true });
            console.log(`  created ${dir}/`);
        }
        else {
            console.log(`  exists  ${dir}/`);
        }
    }
    const configPath = path.join(root, 'agentic.config.yaml');
    if (!fs.existsSync(configPath)) {
        fs.writeFileSync(configPath, dumpYaml(DEFAULT_CONFIG), 'utf-8');
        console.log('  created agentic.config.yaml');
    }
    else {
        console.log('  exists  agentic.config.yaml');
    }
    console.log('\nProject initialised. Run `agentic add agent <id>` to add your first agent.');
}
//# sourceMappingURL=init.js.map