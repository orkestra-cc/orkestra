import React, { useState } from 'react';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Button, Card, Collapse } from 'react-bootstrap';

const ProfileIntro: React.FC = () => {
  const [collapsed, setCollapsed] = useState(true);
  return (
    <Card className="mb-3">
      <Card.Header className="bg-body-tertiary">
        <h5 className="mb-0">Intro</h5>
      </Card.Header>

      <Card.Body className="text-1000">
        <p>
          Dedicated, passionate, and accomplished Full Stack Developer with 9+
          years of progressive experience working as an Independent Contractor
          for Google and developing and growing my educational social network
          that helps others learn programming, web design, game development,
          networking.
        </p>
        <Collapse in={collapsed}>
          <div>
            <p>
              I've acquired a wide depth of knowledge and expertise in using my
              technical skills in programming, computer science, software
              development, and mobile app development to developing solutions to
              help organizations increase productivity, and accelerate business
              performance.
            </p>
            <p>
              It's great that we live in an age where we can share so much with
              technology but I'm but I'm ready for the next phase of my career,
              with a healthy balance between the virtual world and a workplace
              where I help others face-to-face.
            </p>
            <p>
              There's always something new to learn, especially in IT-related
              fields. People like working with me because I can explain
              technology to everyone, from staff to executives who need me to
              tie together the details and the big picture. I can also implement
              the technologies that successful projects need.
            </p>
          </div>
        </Collapse>
      </Card.Body>

      <Card.Footer className="bg-body-tertiary p-0 border-top d-grid">
        <Button
          variant="link"
          // className="me-2 mb-1"
          onClick={() => setCollapsed(!collapsed)}
        >
          Show {collapsed ? 'less' : 'more'}
          <FontAwesomeIcon
            icon="chevron-down"
            className="ms-2 fs-11"
            transform={collapsed ? 'rotate-180' : ''}
          />
        </Button>
      </Card.Footer>
    </Card>
  );
};

export default ProfileIntro;
