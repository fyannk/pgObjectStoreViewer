import React from 'react';
import Layout from '@theme/Layout';
import Link from '@docusaurus/Link';

export default function Home(): React.ReactElement {
  return (
    <Layout
      title="ObjectStoreViewer"
      description="Read-only structural evidence about PostgreSQL backup repositories in object storage">
      <main
        style={{
          maxWidth: 'var(--ifm-container-width)',
          margin: '0 auto',
          padding: '4rem 1rem',
        }}>
        <h1>ObjectStoreViewer</h1>
        <p>
          A read-only web application for inspecting PostgreSQL backup
          repositories in object storage. It scans one configured Barman Cloud
          or pgBackRest repository root over S3, Azure Blob Storage, or GCS and
          publishes a bounded, conservative inventory: backups, WAL continuity,
          timelines, and observed recovery coverage.
        </p>
        <p>
          <strong>
            ObjectStoreViewer reports structural evidence. It does not prove
            that a restore will succeed.
          </strong>
        </p>
        <p>
          <Link className="button button--primary button--lg" to="/docs/">
            Read the documentation
          </Link>
        </p>
      </main>
    </Layout>
  );
}
