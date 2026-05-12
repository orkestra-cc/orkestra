import React, { useState } from 'react';
import PageHeader from 'components/common/PageHeader';
import { Link } from 'react-router';
import { Card, Col, Row, Spinner } from 'react-bootstrap';
import PricingDefaultHeader from './PricingDefaultHeader';
import PricingDefaultCard from './PricingDefaultCard';
import useFakeFetch from 'hooks/ui/useFakeFetch';
import { pricingData } from 'data/pricing';
import FaqBasicCard from 'reference/pages/faq/faq-basic/FaqBasicCard';
import { faqs as faqsData } from 'data/faqs';

// Type definitions for Pricing page
interface PricingFeature {
  id: number;
  title: string;
  tag?: {
    label: string;
    type: string;
  };
}

interface PricingPlan {
  id: number;
  title: string;
  subTitle?: string;
  price: number;
  url: string;
  buttonText: string;
  isFeatured: boolean;
  featureTitle: string;
  features: PricingFeature[];
}

interface FAQ {
  id: number;
  title: string;
  description: string;
}

const PricingDefault: React.FC = () => {
  const [faqs] = useState<FAQ[]>(faqsData);
  const { loading: priceLoading, data: pricing } = useFakeFetch<PricingPlan[]>(
    pricingData,
    1000
  );

  return (
    <>
      <PageHeader
        preTitle="Free for 30 days"
        title="For teams of all sizes, in the cloud"
        description="Get the power, control, and customization you need to manage your <br class='d-none d-md-block' /> team’s and organization’s projects."
        className="mb-3"
        titleTag="h2"
      >
        <Link className="btn btn-sm btn-link ps-0" to="#!">
          Have questions? Chat with us
        </Link>
      </PageHeader>
      <Card className="mb-3">
        <Card.Body>
          <Row className="g-0">
            <PricingDefaultHeader />
            {priceLoading ? (
              <Col xs={12} className="py-4">
                <Spinner
                  className="position-absolute start-50"
                  animation="grow"
                />
              </Col>
            ) : (
              pricing.map((pricingPlan: PricingPlan) => (
                <PricingDefaultCard key={pricingPlan.id} pricing={pricingPlan} />
              ))
            )}
            <Col xs={12} className="text-center">
              <h5 className="mt-5">
                Looking for personal or small team task management?
              </h5>
              <p className="fs-8">
                Try the <Link to="#!">basic version</Link> of Orkestra
              </p>
            </Col>
          </Row>
        </Card.Body>
      </Card>
      <FaqBasicCard
        faqs={faqs}
        header
        headerText="Frequently asked questions"
        bodyClass="bg-body-tertiary"
      />
    </>
  );
};

export default PricingDefault;
