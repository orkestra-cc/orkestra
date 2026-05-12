
import PageHeader from 'components/common/PageHeader';
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
import Background from 'components/common/Background';
import gallery2 from 'assets/img/gallery/2.jpg';
import beachMp4 from 'assets/video/beach.mp4';
import beachWebm from 'assets/video/beach.webm';
import beachImage from 'assets/video/beach.jpg';

const imageCode = `<div className="position-relative py-6 py-lg-8" data-bs-theme="light">
  <Background image={gallery2} overlay="1" className="rounded-soft" />
  <div className="position-relative text-center">
    <h4 className="text-white">Image Background</h4>
  </div>
</div>`;

const videoCode = `<div className="position-relative" data-bs-theme="light">
  <Background video={[ beachMp4, beachWebm]} image={ beachImage } overlay="2" className="rounded-soft" />
  <div className="position-relative vh-75 d-flex flex-center">
    <h4 className="text-white">Video Background</h4>
  </div>
</div>`;

const Backgrounds = () => (
  <>
    <PageHeader
      title="Background"
      description="These modular elements can be readily used and customized in every layout across pages."
      className="mb-3"
    />

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Image Background" />
      <OrkestraComponentCard.Body
        code={imageCode}
        scope={{ Background, gallery2 }}
        language="jsx"
      />
    </OrkestraComponentCard>

    <OrkestraComponentCard noGuttersBottom>
      <OrkestraComponentCard.Header title="Video Background" />
      <OrkestraComponentCard.Body
        code={videoCode}
        scope={{ Background, beachMp4, beachWebm, beachImage }}
        language="jsx"
      />
    </OrkestraComponentCard>
  </>
);

export default Backgrounds;
