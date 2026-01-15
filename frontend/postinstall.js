import fse from 'fs-extra';
import path, { dirname } from 'path';
// const topDir = import.meta.dirname;
import { fileURLToPath } from 'url';
const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);
fse.emptyDirSync(path.join(__dirname, 'public', 'tinymce'));
fse.copySync(
  path.join(__dirname, 'node_modules', 'tinymce'),
  path.join(__dirname, 'public', 'tinymce'),
  { overwrite: true }
);
