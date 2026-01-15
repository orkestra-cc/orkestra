import illustration1 from 'assets/img/icons/spot-illustrations/50.png';
import illustration2 from 'assets/img/icons/spot-illustrations/49.png';
import illustration3 from 'assets/img/icons/spot-illustrations/48.png';
import illustration1Dark from 'assets/img/icons/spot-illustrations/50_dark.png';
import illustration2Dark from 'assets/img/icons/spot-illustrations/49_dark.png';
import illustration3Dark from 'assets/img/icons/spot-illustrations/48_dark.png';

export default [
  {
    icon: ['far', 'lightbulb'],
    iconText: 'PLAN',
    color: 'danger',
    title: 'Blueprint & design',
    description:
      'With Falcon as your guide, now you have a fine-tuned state of the earth tool to make your wireframe a reality.',
    image: illustration1,
    imageDark: illustration1Dark
  },
  {
    icon: ['far', 'object-ungroup'],
    iconText: 'BUILD',
    color: 'info',
    title: '38 Sets of components',
    description:
      "Build any UI effortlessly with Falcon's robust set of layouts, 38 sets of built-in elements, carefully chosen colors, typography, and css helpers.",
    image: illustration2,
    imageDark: illustration2Dark,
    inverse: true
  },
  {
    icon: ['far', 'paper-plane'],
    iconText: 'DEPLOY',
    color: 'success',
    title: 'Review and test',
    description:
      'From IE to iOS, rigorously tested and optimized Falcon will give the near perfect finishing to your webapp; from the landing page to the logout screen.',
    image: illustration3,
    imageDark: illustration3Dark
  }
];
