import path from 'path';
import fs from 'fs';
import * as sass from 'sass';
import rtlcss from 'rtlcss';

const compileSCSS = () => ({
  name: 'compile-scss',
  configureServer(server) {
    // Vitest spins up an in-process Vite server purely to transform test
    // modules; no test imports SCSS, so recompiling theme.scss on every
    // run just pollutes stderr with hundreds of Bootstrap deprecations.
    if (process.env.VITEST) return;

    const scssWatcher = server.watcher;
    // chokidar v4 (bundled by Vite 7) dropped glob support — a glob passed to
    // add() is treated as a literal path that doesn't exist, so no SCSS edit
    // ever fired the handler and the theme only recompiled on server boot.
    // Watch the directory instead; the 'change' handler below already filters
    // on the .scss extension.
    const scssDir = path.resolve(__dirname, 'src/assets/scss');
    scssWatcher.add(scssDir);

    const scssFiles = [path.resolve(__dirname, 'src/assets/scss/theme.scss')];

    const compileSCSSToCSS = async file => {
      const result = await sass.compileAsync(file, {
        style: 'expanded',
        quietDeps: true
      });
      const fileName = path.basename(file, path.extname(file));

      // Path for LTR CSS. sass emits no trailing newline — append one so the
      // tracked artifacts match their committed (newline-terminated) form
      // instead of going one byte dirty on every compile.
      const cssPath = path.resolve(__dirname, `public/css/${fileName}.css`);
      fs.mkdirSync(path.dirname(cssPath), { recursive: true });
      fs.writeFileSync(cssPath, `${result.css}\n`);

      // Generate RTL CSS from LTR CSS
      const rtlResult = rtlcss.process(result.css);
      const rtlCssPath = path.resolve(
        __dirname,
        `public/css/${fileName}.rtl.css`
      );
      fs.writeFileSync(rtlCssPath, `${rtlResult}\n`);
    };

    scssWatcher.on('change', file => {
      if (file.endsWith('.scss')) {
        scssFiles.map(file => {
          compileSCSSToCSS(file);
        });
        server.hot.send({
          type: 'full-reload'
        });
      }
    });

    scssFiles.map(file => {
      compileSCSSToCSS(file);
    });
  }
});

export default compileSCSS;
