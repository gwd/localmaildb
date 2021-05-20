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

create table if not exists lmdb_addresses(
    addressid    integer primary key,
    personalname text,
    mailboxname  text,
    hostname     text,
    unique(personalname, mailboxname, hostname));

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
select strftime("%Y-%m", date) as ts,
       count(*)
  from lmdb_messages
  group by ts
  order by ts desc;

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

