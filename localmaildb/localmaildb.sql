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
