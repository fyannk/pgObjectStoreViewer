import React from 'react';
import Layout from '@theme/Layout';
import Link from '@docusaurus/Link';
import useBaseUrl from '@docusaurus/useBaseUrl';

const capabilities = [
  {
    title: 'Read-only by construction',
    body: 'The application lists, inspects, and reads bounded metadata. There is no upload, delete, restore, or mutation surface anywhere in the codebase — not disabled by configuration, absent by design.',
  },
  {
    title: 'Honest about uncertainty',
    body: 'Incomplete, stale, truncated, or unsupported evidence stays unknown. A partial scan cannot produce a healthy result, and a repository it cannot parse fails visibly rather than quietly.',
  },
  {
    title: 'WAL-aware',
    body: 'Compact WAL ranges, timeline history, duplicates, partial files, and candidate or confirmed gaps — the continuity questions you would otherwise answer by listing object keys by hand.',
  },
  {
    title: 'Cloud-neutral',
    body: 'One evidence model over S3, Azure Blob Storage, and GCS. Provider SDK types stay inside their adapter, and every adapter passes the same shared contract suite.',
  },
];

export default function Home(): React.ReactElement {
  return (
    <Layout
      title="ObjectStoreViewer"
      description="Read-only structural evidence about PostgreSQL backup repositories in object storage">
      <header className="osv-hero">
        <div className="osv-hero__inner">
          <img
            className="osv-hero__mark"
            src={useBaseUrl('img/logo.png')}
            alt=""
            width={220}
            height={166}
          />
          <div className="osv-hero__copy">
            <h1 className="osv-hero__title">
              pg<span>OSV</span>
            </h1>
            <p className="osv-hero__tagline">
              See what is really present in your PostgreSQL backup repository.
            </p>
            <div className="osv-hero__actions">
              <Link className="button button--primary button--lg" to="/docs/">
                Read the documentation
              </Link>
              <Link
                className="button button--outline button--lg osv-hero__ghost"
                to="https://github.com/fyannk/pgObjectStoreViewer">
                View on GitHub
              </Link>
            </div>
          </div>
        </div>
      </header>

      <main className="osv-main">
        <p className="osv-lede">
          ObjectStoreViewer scans one configured Barman Cloud or pgBackRest
          repository root over S3, Azure Blob Storage, or GCS and publishes a
          bounded, conservative inventory: backups, WAL continuity, timelines,
          and observed recovery coverage. It turns object-store metadata into
          something you can read at a glance — completed, running, failed,
          malformed, missing, or unsupported — without digging through object
          keys by hand.
        </p>

        <div className="osv-grid">
          {capabilities.map((c) => (
            <section className="osv-card" key={c.title}>
              <h2>{c.title}</h2>
              <p>{c.body}</p>
            </section>
          ))}
        </div>

        <aside className="osv-note">
          <strong>What it does not claim.</strong> ObjectStoreViewer reports
          structural evidence: what the object store contains and what the
          repository metadata says about it. It does not prove that a restore
          will succeed, and it never reads or exposes your database contents.
        </aside>
      </main>
    </Layout>
  );
}
