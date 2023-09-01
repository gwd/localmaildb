/****************************************
 * Mapping emails to people and companies
 ****************************************/

create table if not exists idmap.person(
    personid    integer primary key,
    personname  text not null, /* "Canonical" name for the person.  Might not be unique. */
    persondesc  text           /* Anything useful to distinguish this person from someone else w/ the same name */
);

create table if not exists idmap.companies(
    companyid   integer primary key,
    companyname text not null,
    companydesc text
);

/*
 * This should only be used where the email address reliable indicates employer;
 * e.g., citrix.com -> Citrix, but gmail.com !-> Google.
 */
create table if not exists idmap.hostname_to_company(
    hostname    text not null,
    companyid   integer not null,
    unique(hostname),
    foreign key(companyid) references companies
);

create table if not exists idmap.person_to_company(
    personid  integer not null,
    companyid integer not null,
    startdate date, /* NULL here means 'for as  long as we know' */
    enddate   date, /* NULL here means "currently" */
    unique(personid, companyid, startdate, enddate),
    foreign key(personid) references person,
    foreign key(companyid) references companies
);

create table if not exists idmap.address_to_person(
    mailboxname text not null,
    hostname    text not null,
    personid integer not null,
    primary key(mailboxname, hostname)
    unique(mailboxname, hostname, personid),
    foreign key(personid) references person
);

create table if not exists idmap.tags(
    tagid integer primary key,
    tagname text not null,
    tagdesc text
);

create table if not exists idmap.address_to_tag(
    mailboxname text not null,
    hostname    text not null,
    tagid integer not null,
    primary key(mailboxname, hostname)
    unique(mailboxname, hostname, tagid),
    foreign key(tagid) references tags
);

/* Link a list of address ids to a person.  Left join so that you get
 * an error if you mistype the person name. */
insert into idmap.address_to_person(mailboxname, hostname, personid) 
    select * from (values ('JBeulich', 'suse.com'), ('jbeulich', 'suse.com'))
        left join (select personid
	          from idmap.person
		  where personname="Jan Beulich");

/* Same as above but for tags */
insert into idmap.address_to_tag(mailboxname, hostname, tagid) 
    select * from (values ('osstest-admin', 'xenproject.org'), ('citrix-osstest', 'xenproject.org'), ('osstest', 'xenbits.xen.org'))
        left join (select tagid
	          from idmap.tags
		  where tagname='bot');

/* Map people to known companies at a specific date */
select personname, companyname
  from person
    natural join person_to_company
    natural join companies
  where '2017-02-23' between IFNULL(startdate, '0000-01-01') and IFNULL(enddate, '9999-12-31');

select personname,
       (case when '2017-02-23' between IFNULL(startdate, '0000-01-01') and IFNULL(enddate, '9999-12-31') then companyname end)
  from person
    natural join person_to_company
    natural join companies;

/* Map an email address to a company if no hostname matches */
select distinct mailboxname, hostname, startdate, enddate
  from lmdb_addresses
    natural join address_to_person
    natural join person
    natural join person_to_company
    natural join companies
  where hostname not in (select hostname from hostname_to_company);
  
/* 
 * Map <address, date> to a company using hostname if available; falling back to personal work
 * map; falling back to 'unknown'.   
 */
with annotated_messages as
  (select *, coalesce(hostcompany, max(personcompany), 'Unknown') as companyname
    from (
      select *,
        case when date between IFNULL(startdate, '0000-01-01') and IFNULL(enddate, '9999-12-31') then personcompanyinner end as personcompany
      from lmdb_messages
        natural join lmdb_envelopejoin
        natural join lmdb_addresses
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
select messageid, mailboxname, hostname, date, companyname
  from annotated_messages
  order by random()
  limit 30;

/*
 * Addresses not linked to a company (either by hostname or person), ordered by volume
 */

with annotated_messages as
  (select *, coalesce(hostcompany, max(personcompany), 'Unknown') as companyname
    from (
      select *,
        case when date between IFNULL(startdate, '0000-01-01') and IFNULL(enddate, '9999-12-31') then personcompanyinner end as personcompany
      from lmdb_messages
        natural join lmdb_envelopejoin
        natural join lmdb_addresses
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
select personalname, mailboxname || '@' || hostname as address, count(*) as n
  from annotated_messages
  where companyname='Unknown'
  group by address
  order by n desc
  limit 50;


/* SCRATCH */

insert into idmap.address_to_person(mailboxname, hostname, personid) 
    select * from (values ('JBeulich', 'suse.com'), ('jbeulich', 'suse.com'))
        left join (select personid
	          from idmap.person
		  where personname="Jan Beulich");

/* Add people to the 'person' table based on the 'personalname' in the
 * email address */
with nonbot_addresses as
  (select * from lmdb_addresses
     left natural join
       (select *
          from idmap.address_to_tag
	    natural join idmap.tags
	    where tagname='bot')
    where tagid is NULL)
insert into person(personname)
select personalname/*, personname, mailboxname, hostname, personid*/
  from lmdb_messages
    natural join lmdb_envelopejoin
    natural join nonbot_addresses
    left join person on personalname=personname
    left natural join (select personid as a2pid, mailboxname, hostname from address_to_person)
  where envelopepart=1 and date >= date('now', '-1 year') and a2pid is null
    and hostname not in ('redhat.com')
    and personalname not in ('Borislav Petkov', 'Vishal Moola (Oracle)', 'Thomas Gleixner', 'Suren Baghadasaryan', 'Carlo Nonato', 'Bernhard Beschow', 'Andy Shevchenko')
  group by personalname,mailboxname,hostname
  having count(*) > 50
  order by personalname;

/* Add address_to_person mappings where the 'personalname' of the address maps
 * 'personname' in the 'person' table */
with nonbot_addresses as
  (select * from lmdb_addresses
     left natural join
       (select *
          from idmap.address_to_tag
	    natural join idmap.tags
	    where tagname='bot')
    where tagid is NULL)
insert into idmap.address_to_person(mailboxname, hostname, personid)
select mailboxname, hostname, personid
  from lmdb_messages
    natural join lmdb_envelopejoin
    natural join nonbot_addresses
    join person on personalname=personname
    left natural join (select personid as a2pid, mailboxname, hostname from address_to_person)
  where envelopepart=1 and date >= date('now', '-1 year') and a2pid is null
  group by mailboxname,hostname;

