import path from 'path';
import fs from 'fs';
import * as sass from 'sass';
import rtlcss from 'rtlcss';

const compileSCSS = () => ({
  name: 'compile-scss',
  configureServer(server) {
    const scssWatcher = server.watcher;
    const scssGlob = path.resolve(__dirname, 'src/assets/scss/**/*.scss');
    scssWatcher.add(scssGlob);

    const scssFiles = [path.resolve(__dirname, 'src/assets/scss/theme.scss')];

    const compileSCSSToCSS = async file => {
      const result = await sass.compileAsync(file, { style: 'expanded' });
      const fileName = path.basename(file, path.extname(file));

      // Path for LTR CSS
      const cssPath = path.resolve(__dirname, `public/css/${fileName}.css`);
      fs.mkdirSync(path.dirname(cssPath), { recursive: true });
      fs.writeFileSync(cssPath, result.css);

      // Generate RTL CSS from LTR CSS
      const rtlResult = rtlcss.process(result.css);
      const rtlCssPath = path.resolve(
        __dirname,
        `public/css/${fileName}.rtl.css`
      );
      fs.writeFileSync(rtlCssPath, rtlResult);
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
