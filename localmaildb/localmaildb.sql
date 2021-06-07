create table if not exists lmdb_params(
       key       text primary key,
       value     text not null);
/* 
 * Parameters:
 * - dbversion: Curently '1'
 */


create table if not exists lmdb_messages(
    messageid text primary key,
    subject   text not null,
    date      date  not null,
    message   text not null,
    inreplyto text,
    size      integer  not null);

/* FIXME: These aren't in the code yet */
create index lmdb_messages_date on lmdb_messages(date);
create index lmdb_messages_inreplyto on lmdb_messages(inreplyto);

create table if not exists lmdb_addresses(
    addressid    integer primary key,
    personalname text,
    mailboxname  text,
    hostname     text,
    unique(personalname, mailboxname, hostname));

create index lmdb_addresses_email on lmdb_addresses(mailboxname, hostname);

/*
 * envelope part should be one of the following:
 * 	HeaderPartFrom    = HeaderPart(1)
 *	HeaderPartSender  = HeaderPart(2)
 *	HeaderPartReplyTo = HeaderPart(3)
 *	HeaderPartTo      = HeaderPart(4)
 *	HeaderPartCc      = HeaderPart(5)
 *	HeaderPartBcc     = HeaderPart(6)
 */
create table if not exists lmdb_envelopejoin(
    messageid text not null,
    addressid integer not null,
    envelopepart integer not null, /* FIXME: This should probably be 'field' */
    foreign key(messageid) references lmdb_messages,
    foreign key(addressid) references lmdb_addresses)

/* FIXME: Not in the code yet */
create index lmdb_envelopejoin_messageid on lmdb_envelopejoin(messageid);

create table if not exists lmdb_mailboxes(
    mailboxid integer primary key,
    mailboxname text);

create table if not exists lmdb_mailbox_join(
    mailboxid integer,
    messageid text,
    foreign key(mailboxid) references lmdb_mailboxes,
    foreign key(messageid) references lmdb_messages);

/*
 * RECIPES
 */

/* Date, sender, subject */
select date,
       personalname,
       mailboxname,
       hostname,
       subject
    from lmdb_messages
      natural join lmdb_envelopejoin
      natural join lmdb_addresses
    where envelopepart=1;

/* Count sender emails */
select mailboxname, hostname, count(*) as n
    from lmdb_messages
      natural join lmdb_envelopejoin
      natural join lmdb_addresses
    where envelopepart=1
    group by mailboxname,hostname
    order by n desc
    limit 30;

/* Histogram of messages sent by month */
select strftime("%Y-%m", date) as month,
       count(*)
  from lmdb_messages
  group by month
  order by month asc;

/* Histogram of messages per month, sent by addresses tagged as 'bot' */
select strftime("%Y-%m", date) as ts,
       count(*)
  from lmdb_messages
    natural join lmdb_envelopejoin
    natural join lmdb_addresses
    natural join idmap.address_to_tag
    natural join idmap.tags
  where envelopepart=1 /* Restrict to 'From' addresses */
    and tagname='bot'
  group by ts
  order by ts desc;

/* All addresses not tagged as 'bot' */
select mailboxname, hostname
   from lmdb_addresses
     left natural join
       (select *
          from idmap.address_to_tag
	    natural join idmap.tags
	    where tagname='bot')
    where tagid is NULL
    limit 20;
       

/* Histogram of messages per month, sent by addresses tagged as 'bot' */
with nonbot_addresses as
  (select * from lmdb_addresses
     left natural join
       (select *
          from idmap.address_to_tag
	    natural join idmap.tags
	    where tagname='bot')
    where tagid is NULL)
select strftime("%Y-%m", date) as ts,
       count(*)
  from lmdb_messages
    natural join lmdb_envelopejoin
    natural join nonbot_addresses
  where envelopepart=1 /* Restrict to 'From' addresses */
  group by ts
  order by ts desc;

/* Messages with the company name by hostname, "Unknown" if unknown */
select date, mailboxname, hostname, subject, IFNULL(companyname, "Unknown") as company
  from lmdb_messages
    natural join lmdb_envelopejoin
    natural join lmdb_addresses
    left natural join (select * from hostname_to_company natural join companies)
  where envelopepart=1
  order by random()
  limit 30;

/* Summary of all messages classified by company */
select IFNULL(companyname, "Unknown") as company, count(*) as n
  from lmdb_messages
    natural join lmdb_envelopejoin
    natural join lmdb_addresses
    left natural join (select * from hostname_to_company natural join companies)
  where envelopepart=1
  group by company
  order by n desc
  limit 30;

/* Summary of "unknown" hostnames */
select hostname, count(*) as n
  from lmdb_messages
    natural join lmdb_envelopejoin
    natural join lmdb_addresses
    left natural join (select * from hostname_to_company natural join companies)
  where envelopepart=1
    and companyname is NULL
  group by hostname
  order by n desc
  limit 30;

/* Messages with the person, or email if no person defined */
select date, IFNULL(personname, mailboxname || "@" || hostname) as id, subject, IFNULL(companyname, "Unknown") as company
  from lmdb_messages
    natural join lmdb_envelopejoin
    natural join lmdb_addresses
    left natural join (select * from hostname_to_company natural join companies)
    left natural join (select * from address_to_person natural join person)
  where envelopepart=1
  order by random()
  limit 20;

select IFNULL(personname, mailboxname || "@" || hostname) as id, IFNULL(companyname, "Unknown") as company, count(*) as messages
  from lmdb_messages
    natural join lmdb_envelopejoin
    natural join lmdb_addresses
    left natural join (select * from hostname_to_company natural join companies)
    left natural join (select * from address_to_person natural join person)
  where envelopepart=1
  group by id
  order by messages desc
  limit 50;

/* Find out top addresses of 'Unknown' contributors so we can maybe classify them */
select mailboxname || "@" || hostname as id, IFNULL(companyname, "Unknown") as company, count(*) as messages
  from lmdb_messages
    natural join lmdb_envelopejoin
    natural join lmdb_addresses
    left natural join (select * from hostname_to_company natural join companies)
  where envelopepart=1 and company="Unknown"
  group by id
  order by messages desc
  limit 50;

/* Histogram of messages sent by company, no personmap */
with nonbot_addresses as
  (select * from lmdb_addresses
     left natural join
       (select *
          from idmap.address_to_tag
	    natural join idmap.tags
	    where tagname='bot')
    where tagid is NULL)
select strftime("%Y-%m", date) as month,
       sum(companyname='Citrix') as Citrix,
       sum(companyname='SUSE') as SUSE,
       sum(companyname='Oracle') as Oracle,
       sum(companyname='Intel') as Intel,
       sum(companyname='AMD') as AMD,
       sum(companyname='Amazon') as Amazon,
       sum(companyname='ARM') as ARM,
       sum(companyname='RedHat') as RedHat,
       sum(companyname='IBM') as IBM,
       sum(companyname='Google') as Google,
       sum(companyname='Nvidia') as Nvidia,
       sum(companyname is null) as Unknown
  from lmdb_messages
    natural join lmdb_envelopejoin
    natural join nonbot_addresses
    left natural join (select * from hostname_to_company natural join companies)
  where envelopepart=1 /* Restrict to 'From' addresses */
  group by month
  order by month asc;

/* With person-company map */
with nonbot_addresses as
  (select * from lmdb_addresses
     left natural join
       (select *
          from idmap.address_to_tag
	    natural join idmap.tags
	    where tagname='bot')
    where tagid is NULL),
  annotated_messages as
  (select *, coalesce(hostcompany, max(personcompany), 'Unknown') as companyname
    from (
      select *,
        case when date between IFNULL(startdate, '0000-01-01') and IFNULL(enddate, '9999-12-31') then personcompanyinner end as personcompany
      from lmdb_messages
        natural join lmdb_envelopejoin
        natural join nonbot_addresses
        left natural join (
          select hostname, companyname as hostcompany
            from hostname_to_company natural join companies)
        left natural join (
          select mailboxname, hostname, startdate, enddate, companyname as personcompanyinner
            from address_to_person
	      natural join person
	      natural join person_to_company
	      natural join companies))
    where envelopepart=1
    group by messageid)
select strftime("%Y-%m", date) as month,
       sum(companyname='Citrix') as Citrix,
       sum(companyname='SUSE') as SUSE,
       sum(companyname='Oracle') as Oracle,
       sum(companyname='Intel') as Intel,
       sum(companyname='AMD') as AMD,
       sum(companyname='Amazon') as Amazon,
       sum(companyname='ARM') as ARM,
       sum(companyname='RedHat') as RedHat,
       sum(companyname='IBM') as IBM,
       sum(companyname='Google') as Google,
       sum(companyname='Nvidia') as Nvidia,
       sum(companyname not in ('Citrix', 'SUSE', 'Oracle', 'Intel', 'AMD', 'Amazon', 'ARM', 'RedHat', 'IBM', 'Google', 'Nvidia'))
  from annotated_messages
  group by month
  order by month asc;

/* Simple mail classification */
select messageid, date, mailboxname, hostname, subject,
  GLOB('*[[]*PATCH*[]]*', subject) as patch,
  GLOB('R[eE]:*', subject) as reply
  from lmdb_messages
    natural join lmdb_envelopejoin
    natural join lmdb_addresses
  order by random()
  limit 50;

/* Histogram of imple mail classification */
with nonbot_addresses as
  (select * from lmdb_addresses
     left natural join
       (select *
          from idmap.address_to_tag
	    natural join idmap.tags
	    where tagname='bot')
    where tagid is NULL)
select strftime('%Y-%m', date) as month,
    sum(patch and not reply) as patch,
    sum(patch and reply) as pdisc,
    sum(not patch and not reply) as nonpatch,
    sum(not patch and reply) as nonpatchdisc
  from (
    select date,
      GLOB('*[[]*PATCH*[]]*', subject) as patch,
      GLOB('R[eE]:*', subject) as reply
    from lmdb_messages
      natural join lmdb_envelopejoin
      natural join nonbot_addresses
    where envelopepart=1)
  group by month
  order by month asc;


/* Messages CC'd to george.dunlap@* from dfaggioli@* with PATCH in the title and no RE: */
select date, subject, messageid
  from lmdb_messages
    natural join (
      select messageid
        from lmdb_envelopejoin natural join lmdb_addresses
        where envelopepart=5 and mailboxname='george.dunlap' and hostname='citrix.com')
    natural join (
      select messageid
        from lmdb_envelopejoin natural join lmdb_addresses
        where envelopepart=1 and mailboxname='dfaggioli' and hostname='suse.com')
  where GLOB('*[[]*PATCH*[]]*', subject)
    and not GLOB('R[eE]:*', subject)
  order by date desc
  limit 10;

/* Write the contents of a set of messages to /tmp/message.inbox */
.headers off
.once /tmp/message.mbox
select message
  from lmdb_messages
  where messageid='<161615605709.5036.4052641880659992679.stgit@Wayrath>';

select orig.date, count(replies.messageid) as replies, orig.subject
  from (select *
          from lmdb_messages
	  where date > '2021-01-01'
	    and GLOB('*[[]*PATCH*[]]*', subject)
            and not GLOB('R[eE]:*', subject)) as orig
    left join lmdb_messages as replies on orig.messageid=replies.inreplyto
  group by orig.messageid
  having replies=0
  order by orig.date;
