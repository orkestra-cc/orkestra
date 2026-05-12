
import processList from 'data/feature/processList';
import Section from 'components/common/Section';
import Process from './Process';
import SectionHeader from './SectionHeader';
import { isIterableArray } from 'helpers/utils';

interface ProcessItem {
  color: string;
  icon: any;
  iconText: string;
  title: string;
  description: string;
  image: string;
  imageDark: string;
  inverse: boolean;
}

const Processes: React.FC = () => (
  <Section>
    <SectionHeader
      title="WebApp theme of the future"
      subtitle="Built on top of Bootstrap 5, super modular Orkestra provides you gorgeous design & streamlined UX for your WebApp."
    />
    {isIterableArray(processList) &&
      (processList as ProcessItem[]).map((process, index) => (
        <Process key={process.color} isFirst={index === 0} {...process} />
      ))}
  </Section>
);

export default Processes;
