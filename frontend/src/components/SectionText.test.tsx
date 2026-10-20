import { render, screen } from '@testing-library/react';
import { SectionText, sectionBlocks } from './SectionText';
import { linePieces, splitMarker } from './Translated';

describe('sectionBlocks', () => {
  it('reads sub-headings, paragraphs and lists', () => {
    expect(sectionBlocks('### TEMP(PIN1): Input\n\nConnect it.\n\n• One\n• Two\n\nEnd.')).toEqual([
      { kind: 'h', text: 'TEMP(PIN1): Input' },
      { kind: 'p', text: 'Connect it.' },
      { kind: 'ul', items: ['One', 'Two'] },
      { kind: 'p', text: 'End.' },
    ]);
  });
  it('reads tables and figure references', () => {
    expect(sectionBlocks('| Pin | Name |\n| --- | --- |\n| 1 | OD \\| OC |\n\n[Figure: Block diagram]')).toEqual([
      { kind: 'table', header: ['Pin', 'Name'], rows: [['1', 'OD | OC']] },
      { kind: 'ref', text: 'Figure: Block diagram' },
    ]);
  });
  it('translates table cells and captions, not the markup', () => {
    const row = linePieces('| 1 | 电源地 |');
    expect(row.texts).toEqual([' 1 ', ' 电源地 ']);
    expect(row.build([' 1 ', 'Ground'])).toBe('| 1 | Ground |');
    const ref = linePieces('[Figure: 内部框图]');
    expect(ref.texts).toEqual(['内部框图']);
    expect(ref.build(['Functional block diagram'])).toBe('[Figure: Functional block diagram]');
  });
  it('splits markers for translation', () => {
    expect(splitMarker('### 充电终止')).toEqual(['### ', '充电终止']);
    expect(splitMarker('• 待机电流 40uA')).toEqual(['• ', '待机电流 40uA']);
    expect(splitMarker('plain')).toEqual(['', 'plain']);
  });
});

describe('SectionText', () => {
  it('renders the structure with labels in bold', () => {
    render(<SectionText text={'### Charge termination\n\nNote: the pin floats.\n\n• Standby current 40uA'} />);
    expect(screen.getByRole('heading', { name: 'Charge termination' })).toBeInTheDocument();
    expect(screen.getByText('Note:').tagName).toBe('B');
    expect(screen.getByRole('listitem')).toHaveTextContent('Standby current 40uA');
  });
});
